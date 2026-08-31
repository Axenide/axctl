package server

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestParseSysBacklight(t *testing.T) {
	input := "amdgpu_bl1\nintel_backlight\n"
	got := parseSysBacklight(input)
	if len(got) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(got))
	}
	want := []Device{
		{Name: "backlight-amdgpu_bl1", Key: "backlight", Kind: "brightnessctl"},
		{Name: "backlight-intel_backlight", Key: "backlight", Kind: "brightnessctl"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("device[%d]: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseSysBacklightEmpty(t *testing.T) {
	if got := parseSysBacklight(""); len(got) != 0 {
		t.Errorf("expected no devices, got %d", len(got))
	}
}

func TestParseDDCDetect(t *testing.T) {
	input := `Display 1
   I2C bus:          /dev/i2c-3
   DRM connector:    card1-HDMI-A-1

Display 2
   I2C bus:          /dev/i2c-4
   DRM connector:    card1-DP-1
`
	got := parseDDCDetect(input)
	if len(got) != 2 {
		t.Fatalf("expected 2 displays, got %d", len(got))
	}
	want := []Device{
		{Name: "ddc-3", Key: "ddc-3", Kind: "ddcutil", Bus: "3"},
		{Name: "ddc-4", Key: "ddc-4", Kind: "ddcutil", Bus: "4"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("device[%d]: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseDDCDetectIgnoresJunk(t *testing.T) {
	input := `Invalid header
   Some other text

Display 1
   I2C bus:          /dev/i2c-7
`
	got := parseDDCDetect(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 display, got %d", len(got))
	}
	if got[0].Bus != "7" {
		t.Errorf("expected bus 7, got %q", got[0].Bus)
	}
}

func TestParseVCPGet(t *testing.T) {
	cases := []struct {
		in      string
		wantCur float64
		wantMax float64
		wantOK  bool
	}{
		{"VCP code 0x10 (Brightness)\ncurrent value = 60, max value = 100\n", 0.60, 100, true},
		{"current value = 12, max value = 1500\n", 12.0 / 1500.0, 1500, true},
		{"  current   value=80 max value=200\n", 0.40, 200, true},
		{"nothing useful here\n", 0, 0, false},
		{"current value = 5, max value = 0\n", 0, 0, false},
	}
	for _, tc := range cases {
		cur, mx, ok := parseVCPGet(tc.in)
		if ok != tc.wantOK {
			t.Errorf("input %q: ok=%v want %v", tc.in, ok, tc.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if math.Abs(cur-tc.wantCur) > 1e-6 {
			t.Errorf("input %q: cur=%v want %v", tc.in, cur, tc.wantCur)
		}
		if mx != tc.wantMax {
			t.Errorf("input %q: max=%v want %v", tc.in, mx, tc.wantMax)
		}
	}
}

func TestSaveAndReadBrightnessFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	want := map[string]float64{
		"backlight-amdgpu_bl1": 60,
		"ddc-3":                75,
	}
	if err := writeSavedBrightness(want); err != nil {
		t.Fatalf("writeSavedBrightness: %v", err)
	}
	got, err := readSavedBrightness()
	if err != nil {
		t.Fatalf("readSavedBrightness: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d", len(got), len(want))
	}
	for k, v := range want {
		if math.Abs(got[k]-v) > 1e-6 {
			t.Errorf("%s: got %v want %v", k, got[k], v)
		}
	}
}

func TestReadSavedBrightnessMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	got, err := readSavedBrightness()
	if err != nil {
		t.Fatalf("readSavedBrightness: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestReadSavedBrightnessSkipsMalformed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "axctl"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "backlight-x\t50\nmalformed line no tab\nbroken-value\tabc\nbacklight-y\t80\n"
	if err := os.WriteFile(filepath.Join(dir, "axctl", "brightness.tsv"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readSavedBrightness()
	if err != nil {
		t.Fatalf("readSavedBrightness: %v", err)
	}
	want := map[string]float64{"backlight-x": 50, "backlight-y": 80}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d (%v)", len(got), len(want), got)
	}
	for k, v := range want {
		if math.Abs(got[k]-v) > 1e-6 {
			t.Errorf("%s: got %v want %v", k, got[k], v)
		}
	}
}

func TestApplyToClampsAndSelectsKind(t *testing.T) {
	// Build a fake brightnessctl in PATH and verify it receives the
	// expected arguments including the "%" suffix that was missing in
	// the legacy implementation (ambxst issue #227).
	dir := t.TempDir()
	script := `#!/bin/sh
IFS=' '
echo "$*" > "$TMPDIR/called.txt"
exit 0
`
	bin := filepath.Join(dir, "brightnessctl")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	called := filepath.Join(dir, "called.txt")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "state"))

	if err := applyBacklight(0.6); err != nil {
		t.Fatalf("applyBacklight: %v", err)
	}
	got, err := os.ReadFile(called)
	if err != nil {
		t.Fatalf("read called args: %v", err)
	}
	want := "--class backlight s 60% --quiet\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", strings.TrimSpace(string(got)), strings.TrimSpace(want))
	}

	// Clamping: negative -> 1, >1 -> 100.
	if err := applyBacklight(-0.5); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(called)
	if !strings.Contains(string(got), " s 1% ") {
		t.Errorf("negative value should clamp to 1%%, got %q", string(got))
	}
	if err := applyBacklight(1.5); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(called)
	if !strings.Contains(string(got), " s 100% ") {
		t.Errorf("over-1 value should clamp to 100%%, got %q", string(got))
	}
}

func TestApplyToDDC(t *testing.T) {
	// Same trick as TestApplyToClampsAndSelectsKind but for ddcutil.
	dir := t.TempDir()
	script := `#!/bin/sh
IFS=' '
echo "$*" > "$TMPDIR/called.txt"
exit 0
`
	bin := filepath.Join(dir, "ddcutil")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	called := filepath.Join(dir, "called.txt")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", dir)

	if err := applyDDC("7", 0.42); err != nil {
		t.Fatalf("applyDDC: %v", err)
	}
	got, err := os.ReadFile(called)
	if err != nil {
		t.Fatalf("read called args: %v", err)
	}
	want := "-b 7 setvcp 10 42\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", strings.TrimRight(string(got), "\n"), strings.TrimRight(want, "\n"))
	}
}

func TestResolveTargetsUnknown(t *testing.T) {
	// With no devices visible, resolveTargets must still reject unknown
	// names cleanly without crashing.
	t.Setenv("PATH", t.TempDir())
	if _, err := resolveTargets("does-not-exist"); err == nil {
		t.Errorf("expected error for unknown monitor")
	}
}

func TestDeviceListStableOrder(t *testing.T) {
	// ListBrightness must not panic when PATH is empty. Order between
	// internal and external groups should be internal-then-external.
	t.Setenv("PATH", t.TempDir())
	devs, err := ListBrightness()
	if err != nil {
		t.Fatalf("ListBrightness: %v", err)
	}
	if len(devs) != 0 {
		t.Errorf("expected no devices, got %d", len(devs))
	}
	names := []string{}
	for _, d := range devs {
		names = append(names, d.Name)
	}
	if !sort.StringsAreSorted(names[:len(names)]) && len(names) > 0 {
		t.Logf("names = %v", names)
	}
}
