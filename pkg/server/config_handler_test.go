package server

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"axctl/pkg/ipc"
	"axctl/pkg/ipc/hyprland"
	"axctl/pkg/ipc/mango"
	"axctl/pkg/ipc/mock"
	"axctl/pkg/ipc/niri"
)

func TestPathsForCompositor(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	hypr := &hyprland.Hyprland{}
	n := &niri.Niri{}
	m := &mango.Mango{}

	hp := PathsForCompositor(hypr)
	if !strings.HasSuffix(hp.primary, "hypr/axctl.generated.conf") {
		t.Fatalf("hypr primary = %q", hp.primary)
	}
	if !strings.HasSuffix(hp.alt, "hypr/axctl.generated.lua") {
		t.Fatalf("hypr alt = %q", hp.alt)
	}

	np := PathsForCompositor(n)
	if !strings.HasSuffix(np.primary, "niri/axctl.generated.kdl") {
		t.Fatalf("niri primary = %q", np.primary)
	}
	if np.alt != "" {
		t.Fatalf("niri alt = %q, want empty", np.alt)
	}

	mp := PathsForCompositor(m)
	if !strings.HasSuffix(mp.primary, "mango/axctl.generated.conf") {
		t.Fatalf("mango primary = %q", mp.primary)
	}
}

func TestDefaultOutputPathIsHyprland(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	got := DefaultOutputPath()
	if !strings.HasSuffix(got, "ambxst/hyprland.conf") {
		t.Fatalf("DefaultOutputPath = %q", got)
	}
}

type fakeLoader struct {
	*mock.Compositor
	path string
}

func (f *fakeLoader) LoadConfig(path string) error {
	f.path = path
	return f.Compositor.LoadConfig(path)
}

func TestConfigHandlerWritesNiriAndCallsLoadConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	mockComp := mock.NewCompositor()
	loader := &fakeLoader{Compositor: mockComp}
	h := NewConfigHandler(&niri.Niri{})

	h.compositor = loader

	payload := ipc.ConfigUniversal{
		Appearance: ipc.ConfigAppearance{
			Gaps:   &ipc.Gaps{Inner: intPtr(7)},
			Border: &ipc.Border{Width: intPtr(2), ActiveColor: strPtr("#ff0000")},
		},
		ExecOnce: []string{"waybar"},
	}
	if err := h.ApplyConfig(payload); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	want := filepath.Join(dir, "niri", "axctl.generated.kdl")
	if loader.path != want {
		t.Fatalf("LoadConfig called with %q, want %q", loader.path, want)
	}

	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read generated: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "layout {") {
		t.Fatalf("expected layout block, got: %s", content)
	}
	if !strings.Contains(content, "gaps 7") {
		t.Fatalf("expected gaps 7, got: %s", content)
	}
	if !strings.Contains(content, `spawn-at-startup "waybar"`) {
		t.Fatalf("expected spawn-at-startup, got: %s", content)
	}
}

func TestConfigHandlerWritesMangoAndCallsLoadConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	mockComp := mock.NewCompositor()
	loader := &fakeLoader{Compositor: mockComp}

	h := &ConfigHandler{
		compositor: loader,
		generator:  &mango.Generator{},
		paths:      PathsForCompositor(&mango.Mango{}),
	}

	payload := ipc.ConfigUniversal{
		Appearance: ipc.ConfigAppearance{
			Gaps: &ipc.Gaps{Inner: intPtr(5)},
		},
		Keybinds: ipc.ConfigKeybinds{
			Custom: []ipc.Keybind{{
				Modifiers:  []string{"SUPER"},
				Key:        "Q",
				Dispatcher: "exec",
				Argument:   "kitty",
				Enabled:    true,
			}},
		},
		ExecOnce: []string{"waybar"},
	}
	if err := h.ApplyConfig(payload); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	want := filepath.Join(dir, "mango", "axctl.generated.conf")
	if loader.path != want {
		t.Fatalf("LoadConfig called with %q, want %q", loader.path, want)
	}

	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read generated: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "gappih = 5") {
		t.Fatalf("expected gappih = 5, got: %s", content)
	}
	if !strings.Contains(content, "SUPER,Q,spawn,kitty") {
		t.Fatalf("expected spawn keybind, got: %s", content)
	}
	if !strings.Contains(content, "exec-once = waybar") {
		t.Fatalf("expected exec-once, got: %s", content)
	}
}

func TestHyprlandClientDoesNotImplementLoadConfig(t *testing.T) {
	var c ipc.Compositor = &hyprland.Hyprland{}
	type loader interface {
		LoadConfig(path string) error
	}
	if _, ok := c.(loader); ok {
		t.Fatal("Hyprland should not implement LoadConfig so the handler falls back to ReloadConfig")
	}
}

func TestNiriClientImplementsLoadConfig(t *testing.T) {
	var c ipc.Compositor = &niri.Niri{}
	type loader interface {
		LoadConfig(path string) error
	}
	if _, ok := c.(loader); !ok {
		t.Fatal("Niri must implement LoadConfig for the generated config path to be applied")
	}
}

func TestMangoClientImplementsLoadConfig(t *testing.T) {
	var c ipc.Compositor = &mango.Mango{}
	type loader interface {
		LoadConfig(path string) error
	}
	if _, ok := c.(loader); !ok {
		t.Fatal("Mango must implement LoadConfig for the generated config path to be applied")
	}
}

func TestServerLayoutCurrentHandler(t *testing.T) {
	mockComp := mock.NewCompositor()
	mockComp.SetLayouts([]ipc.Layout{
		{Name: "dwindle"},
		{Name: "master", Current: true},
		{Name: "scroller"},
	})
	socketPath := filepath.Join(t.TempDir(), "daemon.sock")
	srv := New(mockComp, socketPath)

	go func() { _ = srv.Start() }()
	defer func() { _ = os.Remove(socketPath) }()

	conn, dialErr := dialServer(t, socketPath)
	if dialErr != nil {
		t.Fatalf("dial: %v", dialErr)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(map[string]interface{}{
		"id": 1, "method": "Layout.Current", "params": map[string]interface{}{},
	}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var resp struct {
		Result ipc.Layout `json:"result"`
		Error  string     `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("error: %s", resp.Error)
	}
	if resp.Result.Name != "master" || !resp.Result.Current {
		t.Fatalf("current = %+v, want master/current", resp.Result)
	}
}

func TestServerLayoutNextAndPrev(t *testing.T) {
	mockComp := mock.NewCompositor()
	mockComp.SetLayouts([]ipc.Layout{
		{Name: "dwindle"},
		{Name: "master", Current: true},
		{Name: "scroller"},
	})
	socketPath := filepath.Join(t.TempDir(), "daemon.sock")
	srv := New(mockComp, socketPath)

	go func() { _ = srv.Start() }()
	defer func() { _ = os.Remove(socketPath) }()

	next := func(wrap interface{}) ipc.Layout {
		t.Helper()
		conn, dialErr := dialServer(t, socketPath)
		if dialErr != nil {
			t.Fatalf("dial: %v", dialErr)
		}
		defer conn.Close()
		params := map[string]interface{}{}
		if wrap != nil {
			params["wrap"] = wrap
		}
		if err := json.NewEncoder(conn).Encode(map[string]interface{}{
			"id": 1, "method": "Layout.Next", "params": params,
		}); err != nil {
			t.Fatalf("encode: %v", err)
		}
		var resp struct {
			Result ipc.Layout `json:"result"`
			Error  string     `json:"error"`
		}
		if err := json.NewDecoder(conn).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Error != "" {
			t.Fatalf("error: %s", resp.Error)
		}
		return resp.Result
	}
	prev := func(wrap interface{}) ipc.Layout {
		t.Helper()
		conn, dialErr := dialServer(t, socketPath)
		if dialErr != nil {
			t.Fatalf("dial: %v", dialErr)
		}
		defer conn.Close()
		params := map[string]interface{}{}
		if wrap != nil {
			params["wrap"] = wrap
		}
		if err := json.NewEncoder(conn).Encode(map[string]interface{}{
			"id": 1, "method": "Layout.Prev", "params": params,
		}); err != nil {
			t.Fatalf("encode: %v", err)
		}
		var resp struct {
			Result ipc.Layout `json:"result"`
			Error  string     `json:"error"`
		}
		if err := json.NewDecoder(conn).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Error != "" {
			t.Fatalf("error: %s", resp.Error)
		}
		return resp.Result
	}

	if got := next(nil); got.Name != "scroller" {
		t.Fatalf("next(nil) = %q, want scroller (wrap default)", got.Name)
	}
	mockComp.SetLayouts([]ipc.Layout{
		{Name: "dwindle"},
		{Name: "master", Current: true},
		{Name: "scroller"},
	})
	if got := next(false); got.Name != "scroller" {
		t.Fatalf("next(wrap=false) from master = %q, want scroller", got.Name)
	}
	mockComp.SetLayouts([]ipc.Layout{
		{Name: "dwindle", Current: true},
		{Name: "master"},
		{Name: "scroller"},
	})
	if got := prev(nil); got.Name != "scroller" {
		t.Fatalf("prev from dwindle = %q, want scroller (wrap default)", got.Name)
	}
	mockComp.SetLayouts([]ipc.Layout{
		{Name: "dwindle", Current: true},
		{Name: "master"},
		{Name: "scroller"},
	})
	if got := prev(false); got.Name != "dwindle" {
		t.Fatalf("prev(wrap=false) from dwindle = %q, want dwindle (no wrap)", got.Name)
	}
}

func TestServerLayoutSetRejectsUnknownName(t *testing.T) {
	mockComp := mock.NewCompositor()
	mockComp.SetLayouts([]ipc.Layout{
		{Name: "dwindle"},
		{Name: "master", Current: true},
	})
	socketPath := filepath.Join(t.TempDir(), "daemon.sock")
	srv := New(mockComp, socketPath)

	go func() { _ = srv.Start() }()
	defer func() { _ = os.Remove(socketPath) }()

	conn, dialErr := dialServer(t, socketPath)
	if dialErr != nil {
		t.Fatalf("dial: %v", dialErr)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(map[string]interface{}{
		"id": 1, "method": "Layout.Set", "params": map[string]interface{}{"name": "nope"},
	}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var resp struct {
		Result interface{} `json:"result"`
		Error  string      `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == "" {
		t.Fatalf("expected error for unknown layout, got result: %+v", resp.Result)
	}
}

func dialServer(t *testing.T, socketPath string) (net.Conn, error) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		conn, err := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
	}
}

func TestServerLayoutListHandler(t *testing.T) {
	mockComp := mock.NewCompositor()
	mockComp.SetLayouts([]ipc.Layout{
		{Name: "dwindle"},
		{Name: "master", Current: true},
		{Name: "scroller"},
	})
	socketPath := filepath.Join(t.TempDir(), "daemon.sock")
	srv := New(mockComp, socketPath)

	go func() { _ = srv.Start() }()
	defer func() { _ = os.Remove(socketPath) }()

	conn, dialErr := dialServer(t, socketPath)
	if dialErr != nil {
		t.Fatalf("dial: %v", dialErr)
	}
	defer conn.Close()

	req := map[string]interface{}{"id": 1, "method": "Layout.List", "params": map[string]interface{}{}}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var resp struct {
		Result ipc.Layouts `json:"result"`
		Error  string      `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("error: %s", resp.Error)
	}
	if len(resp.Result.Items) != 3 {
		t.Fatalf("items = %+v", resp.Result.Items)
	}
	if resp.Result.Active != "master" {
		t.Fatalf("active = %q, want master", resp.Result.Active)
	}
	if resp.Result.Source != ipc.LayoutSourceDynamic {
		t.Fatalf("source = %q, want dynamic", resp.Result.Source)
	}
	if resp.Result.Compositor != "unknown" {
		t.Fatalf("compositor = %q, want unknown", resp.Result.Compositor)
	}
}

func TestConfigHandlerNoGenerator(t *testing.T) {
	mockComp := mock.NewCompositor()
	h := &ConfigHandler{compositor: mockComp}
	err := h.ApplyConfig(ipc.ConfigUniversal{})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected generator error, got %v", err)
	}
}

func intPtr(v int) *int       { return &v }
func strPtr(s string) *string { return &s }
