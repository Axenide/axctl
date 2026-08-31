package server

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Device describes a brightness-controllable output. Internal panels
// (laptop screens, identified by /sys/class/backlight/<dev>) report
// Kind="brightnessctl". External displays reached over DDC/CI report
// Kind="ddcutil" with a numeric Bus (the /dev/i2c-N index).
//
// Name preserves the historical kernel-derived identifier
// (e.g. "backlight-amdgpu_bl1") for back-compat with scripts and tests.
// Key is the canonical monitor identifier used for event broadcasts and
// matches what the QML front-end keys its monitors by: "backlight" for
// any internal panel, "ddc-<bus>" for an external DDC display.
type Device struct {
	Name       string   `json:"name"`
	Key        string   `json:"key,omitempty"`
	Kind       string   `json:"kind"`
	Bus        string   `json:"bus,omitempty"`
	Brightness *float64 `json:"brightness,omitempty"`
}

// MonitorKey returns the canonical monitor identifier for a Device.
// Used by the server to emit Event.BrightnessChanged with a key that
// the QML front-end can match against its BrightnessMonitor entries.
func MonitorKey(d Device) string {
	if d.Key != "" {
		return d.Key
	}
	if d.Kind == "ddcutil" {
		return "ddc-" + d.Bus
	}
	return "backlight"
}

// ddcVCPRe parses the current/max brightness fields emitted by
// `ddcutil getvcp 10`. The two groups swallow the `=` separator and
// any whitespace that broke the previous fields-based parser.
var ddcVCPRe = regexp.MustCompile(`current\s+value\s*=\s*(\d+).*?max\s+value\s*=\s*(\d+)`)

func parseSysBacklight(out string) []Device {
	var devs []Device
	for _, name := range strings.Fields(out) {
		if name == "" {
			continue
		}
		devs = append(devs, Device{
			Name: "backlight-" + name,
			Key:  "backlight",
			Kind: "brightnessctl",
		})
	}
	return devs
}

func parseDDCDetect(out string) []Device {
	var devs []Device
	for _, block := range strings.Split(out, "\n\n") {
		block = strings.TrimSpace(block)
		if !strings.HasPrefix(block, "Display ") {
			continue
		}
		var bus string
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "I2C bus:") {
				if idx := strings.LastIndex(line, "/dev/i2c-"); idx >= 0 {
					bus = line[idx+len("/dev/i2c-"):]
				}
			}
		}
		if bus != "" {
			devs = append(devs, Device{
				Name: "ddc-" + bus,
				Key:  "ddc-" + bus,
				Kind: "ddcutil",
				Bus:  bus,
			})
		}
	}
	return devs
}

func parseVCPGet(out string) (current, max float64, ok bool) {
	m := ddcVCPRe.FindStringSubmatch(out)
	if len(m) < 3 {
		return 0, 0, false
	}
	cur, err1 := strconv.ParseFloat(m[1], 64)
	mx, err2 := strconv.ParseFloat(m[2], 64)
	if err1 != nil || err2 != nil || mx <= 0 {
		return 0, 0, false
	}
	return cur / mx, mx, true
}

func readBacklightCurrent() (*float64, error) {
	out, err := exec.Command("sh", "-c", "brightnessctl g; brightnessctl m").Output()
	if err != nil {
		return nil, err
	}
	fs := strings.Fields(string(out))
	if len(fs) < 2 {
		return nil, fmt.Errorf("brightnessctl output too short")
	}
	cur, err1 := strconv.ParseFloat(fs[0], 64)
	mx, err2 := strconv.ParseFloat(fs[1], 64)
	if err1 != nil || err2 != nil || mx <= 0 {
		return nil, fmt.Errorf("brightnessctl returned invalid numbers")
	}
	v := cur / mx
	return &v, nil
}

func readDDCCurrent(bus string) (*float64, error) {
	out, err := exec.Command("ddcutil", "-b", bus, "getvcp", "10").Output()
	if err != nil {
		return nil, err
	}
	v, _, ok := parseVCPGet(string(out))
	if !ok {
		return nil, fmt.Errorf("could not parse VCP output")
	}
	return &v, nil
}

func lookupInternal() []Device {
	if _, err := exec.LookPath("brightnessctl"); err != nil {
		return nil
	}
	out, err := exec.Command("sh", "-c", "ls /sys/class/backlight/ 2>/dev/null").Output()
	if err != nil {
		return nil
	}
	return parseSysBacklight(string(out))
}

func lookupDDC() []Device {
	if _, err := exec.LookPath("ddcutil"); err != nil {
		return nil
	}
	out, err := exec.Command("ddcutil", "detect", "--brief").Output()
	if err != nil {
		return nil
	}
	return parseDDCDetect(string(out))
}

func applyBacklight(value float64) error {
	pct := int(value*100 + 0.5)
	if pct < 1 {
		pct = 1
	}
	if pct > 100 {
		pct = 100
	}
	return exec.Command("brightnessctl", "--class", "backlight", "s", fmt.Sprintf("%d%%", pct), "--quiet").Run()
}

func applyDDC(bus string, value float64) error {
	pct := int(value*100 + 0.5)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return exec.Command("ddcutil", "-b", bus, "setvcp", "10", strconv.Itoa(pct)).Run()
}

func resolveTargets(name string) ([]Device, error) {
	if name == "" || name == "all" {
		return ListBrightness()
	}
	all, err := ListBrightness()
	if err != nil {
		return nil, err
	}
	if name == "backlight" {
		var out []Device
		for _, d := range all {
			if d.Kind == "brightnessctl" {
				out = append(out, d)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("no internal backlight devices")
		}
		return out, nil
	}
	for _, d := range all {
		if d.Name == name {
			return []Device{d}, nil
		}
	}
	return nil, fmt.Errorf("monitor %q not found", name)
}

func applyTo(d Device, value float64) error {
	if value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}
	switch d.Kind {
	case "brightnessctl":
		return applyBacklight(value)
	case "ddcutil":
		return applyDDC(d.Bus, value)
	default:
		return fmt.Errorf("unknown kind %q", d.Kind)
	}
}

func currentOf(d Device) (*float64, error) {
	switch d.Kind {
	case "brightnessctl":
		return readBacklightCurrent()
	case "ddcutil":
		return readDDCCurrent(d.Bus)
	default:
		return nil, fmt.Errorf("unknown kind %q", d.Kind)
	}
}

// readBroadcastValue returns the post-apply current normalized value of
// d, falling back gracefully when the read fails so the broadcast can
// be skipped rather than emitting a stale or wrong value.
func readBroadcastValue(d Device) (float64, bool) {
	v, err := currentOf(d)
	if err != nil || v == nil {
		return 0, false
	}
	return *v, true
}

// brightnessSaveFile lives under XDG state (UserConfigDir on Linux) so
// restore survives reboots. Format: one line per monitor as
// "<name>\t<percent>\n".
func brightnessSaveFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "axctl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "brightness.tsv"), nil
}

func readSavedBrightness() (map[string]float64, error) {
	path, err := brightnessSaveFile()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]float64{}, nil
		}
		return nil, err
	}
	defer f.Close()
	out := map[string]float64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		idx := strings.IndexByte(line, '\t')
		if idx < 0 {
			continue
		}
		name := line[:idx]
		pct, err := strconv.ParseFloat(line[idx+1:], 64)
		if err != nil {
			continue
		}
		out[name] = pct
	}
	return out, sc.Err()
}

func writeSavedBrightness(values map[string]float64) error {
	path, err := brightnessSaveFile()
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for name, pct := range values {
		if _, err := fmt.Fprintf(w, "%s\t%.0f\n", name, pct); err != nil {
			return err
		}
	}
	return w.Flush()
}

// ListBrightness enumerates all brightness-controllable outputs with
// their current normalized (0..1) value when available. Internal laptop
// panels are read from /sys/class/backlight; external displays are read
// from ddcutil. Returns an empty slice (never nil) when no devices
// are discoverable so JSON callers get `[]` rather than `null`.
func ListBrightness() ([]Device, error) {
	all := []Device{}
	all = append(all, lookupInternal()...)
	all = append(all, lookupDDC()...)
	for i := range all {
		v, err := currentOf(all[i])
		if err == nil && v != nil {
			all[i].Brightness = v
		}
	}
	return all, nil
}

// GetBrightness returns the current normalized (0..1) brightness for a
// named device, or an error if it cannot be read.
func GetBrightness(name string) (float64, error) {
	targets, err := resolveTargets(name)
	if err != nil {
		return 0, err
	}
	d := targets[0]
	v, err := currentOf(d)
	if err != nil || v == nil {
		if err == nil {
			err = fmt.Errorf("no brightness value available")
		}
		return 0, err
	}
	return *v, nil
}

// SetBrightness applies an absolute brightness (0..1) to one or all
// devices. An empty name applies to every known device.
func SetBrightness(name string, value float64) error {
	targets, err := resolveTargets(name)
	if err != nil {
		return err
	}
	for _, d := range targets {
		if err := applyTo(d, value); err != nil {
			return err
		}
	}
	return nil
}

// AdjustBrightness adds a relative delta (clamped to [0.05, 1]) to one
// or all devices.
func AdjustBrightness(name string, delta float64) error {
	targets, err := resolveTargets(name)
	if err != nil {
		return err
	}
	for _, d := range targets {
		cur, err := currentOf(d)
		if err != nil || cur == nil {
			continue
		}
		v := *cur + delta
		if v < 0.05 {
			v = 0.05
		}
		if v > 1 {
			v = 1
		}
		if err := applyTo(d, v); err != nil {
			return err
		}
	}
	return nil
}

// SaveBrightness snapshots the current brightness of one or all known
// devices to the XDG state file. An empty name saves every device.
func SaveBrightness(name string) error {
	saved, err := readSavedBrightness()
	if err != nil {
		return err
	}
	devices, err := resolveTargets(name)
	if err != nil {
		return err
	}
	for _, d := range devices {
		v, err := currentOf(d)
		if err != nil || v == nil {
			continue
		}
		saved[d.Name] = *v * 100
	}
	return writeSavedBrightness(saved)
}

// RestoreBrightness applies previously saved brightness values. An
// empty name restores every saved entry; a specific name restores only
// that device.
func RestoreBrightness(name string) error {
	saved, err := readSavedBrightness()
	if err != nil {
		return err
	}
	if name == "" || name == "all" {
		for n, pct := range saved {
			if err := SetBrightness(n, pct/100); err != nil {
				return err
			}
		}
		return nil
	}
	pct, ok := saved[name]
	if !ok {
		return fmt.Errorf("no saved brightness for %q", name)
	}
	return SetBrightness(name, pct/100)
}
