package niri

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"axctl/pkg/ipc"
)

type fakeHandler func(req json.RawMessage) (any, error)

type fakeNiri struct {
	t        *testing.T
	socket   string
	listener net.Listener
	mu       sync.Mutex
	requests []json.RawMessage
	handler  fakeHandler
	done     chan struct{}
	closed   bool
}

func newFakeNiri(t *testing.T, h fakeHandler) *fakeNiri {
	t.Helper()

	dir := t.TempDir()
	socket := filepath.Join(dir, "niri.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("Listen(%s) error = %v", socket, err)
	}

	f := &fakeNiri{
		t:        t,
		socket:   socket,
		listener: listener,
		handler:  h,
		done:     make(chan struct{}),
	}

	go f.acceptLoop()
	t.Cleanup(func() { f.Close() })
	return f
}

func (f *fakeNiri) Close() {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return
	}
	f.closed = true
	f.mu.Unlock()
	_ = f.listener.Close()
	select {
	case <-f.done:
	case <-time.After(time.Second):
	}
}

func (f *fakeNiri) Client() *Niri {
	f.t.Setenv("NIRI_SOCKET", f.socket)
	c, err := New()
	if err != nil {
		f.t.Fatalf("New() error = %v", err)
	}
	return c
}

func (f *fakeNiri) acceptLoop() {
	defer close(f.done)
	for {
		conn, err := f.listener.Accept()
		if err != nil {
			return
		}
		go f.serve(conn)
	}
}

func (f *fakeNiri) serve(conn net.Conn) {
	defer conn.Close()

	dec := json.NewDecoder(conn)
	var req json.RawMessage
	if err := dec.Decode(&req); err != nil {
		return
	}

	f.mu.Lock()
	f.requests = append(f.requests, req)
	h := f.handler
	f.mu.Unlock()

	resp, err := h(req)
	if err != nil {
		reply := map[string]string{"Err": err.Error()}
		enc := json.NewEncoder(conn)
		_ = enc.Encode(reply)
		return
	}

	if resp == nil {
		reply := map[string]string{"Ok": "Handled"}
		enc := json.NewEncoder(conn)
		_ = enc.Encode(reply)
		_ = conn.Close()
		return
	}

	reply := map[string]json.RawMessage{"Ok": mustJSON(f.t, resp)}
	enc := json.NewEncoder(conn)
	_ = enc.Encode(reply)
	_ = conn.Close()
}

func (f *fakeNiri) requestsSnapshot() []json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]json.RawMessage, len(f.requests))
	copy(out, f.requests)
	return out
}

func mustJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

func jsonEq(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var g, w interface{}
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("unmarshal got: %v: %s", err, string(got))
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("unmarshal want: %v: %s", err, want)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("got %s\nwant %s", string(got), want)
	}
}

func parseRequest(t *testing.T, raw json.RawMessage) interface{} {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("parseRequest: %v", err)
	}
	return v
}

func TestNewRequiresSocket(t *testing.T) {
	t.Setenv("NIRI_SOCKET", "")
	if _, err := New(); err == nil {
		t.Fatal("expected error when NIRI_SOCKET is empty")
	}
}

func TestReplyErrorPropagates(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) {
		return nil, fmt.Errorf("niri rejected this")
	})
	c := f.Client()
	err := c.FocusWindow("42")
	if err == nil || !strings.Contains(err.Error(), "niri rejected this") {
		t.Fatalf("FocusWindow error = %v, want niri error", err)
	}
}

func TestListWindowsParsing(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) {
		var v interface{}
		if err := json.Unmarshal(req, &v); err != nil {
			return nil, err
		}
		s, _ := v.(string)
		switch s {
		case "Workspaces":
			return map[string]interface{}{
				"Workspaces": []map[string]interface{}{
					{"id": float64(2), "idx": float64(1), "output": "DP-1", "is_active": true, "is_focused": true, "is_urgent": false},
				},
			}, nil
		case "Windows":
			return map[string]interface{}{
				"Windows": []map[string]interface{}{
					{
						"id":           float64(7),
						"title":        "Demo",
						"app_id":       "demo.app",
						"workspace_id": float64(2),
						"is_focused":   true,
						"is_floating":  false,
						"is_urgent":    false,
					},
				},
			}, nil
		}
		return nil, fmt.Errorf("unexpected request: %s", string(req))
	})

	c := f.Client()
	windows, err := c.ListWindows()
	if err != nil {
		t.Fatalf("ListWindows() error = %v", err)
	}
	if len(windows) != 1 {
		t.Fatalf("windows = %d, want 1", len(windows))
	}
	w := windows[0]
	if w.ID != "7" || w.Title != "Demo" || w.AppID != "demo.app" {
		t.Fatalf("window identity wrong: %+v", w)
	}
	if w.WorkspaceID != "2" {
		t.Fatalf("workspace_id = %q, want 2", w.WorkspaceID)
	}
	if !w.IsFocused || w.IsFloating || w.IsFullscreen {
		t.Fatalf("flags wrong: focused=%v floating=%v fullscreen=%v", w.IsFocused, w.IsFloating, w.IsFullscreen)
	}
	if w.Metadata["monitor_id"] != "DP-1" {
		t.Fatalf("monitor_id = %v, want DP-1", w.Metadata["monitor_id"])
	}
}

func TestActiveWindowNull(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) {
		return map[string]interface{}{"FocusedWindow": nil}, nil
	})
	c := f.Client()
	id, err := c.ActiveWindow()
	if err != nil {
		t.Fatalf("ActiveWindow() error = %v", err)
	}
	if id != "" {
		t.Fatalf("ActiveWindow() = %q, want empty", id)
	}
}

func TestActiveWindowPresent(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) {
		return map[string]interface{}{
			"FocusedWindow": map[string]interface{}{"id": float64(99)},
		}, nil
	})
	c := f.Client()
	id, err := c.ActiveWindow()
	if err != nil {
		t.Fatalf("ActiveWindow() error = %v", err)
	}
	if id != "99" {
		t.Fatalf("ActiveWindow() = %q, want 99", id)
	}
}

func TestListWorkspacesParsing(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) {
		return map[string]interface{}{
			"Workspaces": []map[string]interface{}{
				{
					"id":               float64(5),
					"idx":              float64(3),
					"name":             "code",
					"output":           "DP-1",
					"is_active":        true,
					"is_focused":       true,
					"is_urgent":        false,
					"active_window_id": float64(42),
				},
			},
		}, nil
	})
	c := f.Client()
	wss, err := c.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces() error = %v", err)
	}
	if len(wss) != 1 {
		t.Fatalf("workspaces = %d, want 1", len(wss))
	}
	ws := wss[0]
	if ws.ID != "5" || ws.Name != "code" || ws.MonitorID != "DP-1" {
		t.Fatalf("workspace identity wrong: %+v", ws)
	}
	if v, _ := ws.Metadata["active_window_id"].(string); v != "42" {
		t.Fatalf("active_window_id = %v, want 42", ws.Metadata["active_window_id"])
	}
}

func TestListMonitorsParsing(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) {
		return map[string]interface{}{
			"Outputs": map[string]interface{}{
				"DP-1": map[string]interface{}{
					"name":           "DP-1",
					"make":           "ACME",
					"model":          "X1",
					"is_custom_mode": false,
					"vrr_supported":  true,
					"vrr_enabled":    false,
					"current_mode":   float64(0),
					"modes": []map[string]interface{}{
						{"width": float64(1920), "height": float64(1080), "refresh_rate": float64(60000), "is_preferred": true},
					},
					"logical": map[string]interface{}{
						"x":         float64(0),
						"y":         float64(0),
						"width":     float64(1920),
						"height":    float64(1080),
						"scale":     1.0,
						"transform": "90",
					},
				},
			},
		}, nil
	})
	c := f.Client()
	monitors, err := c.ListMonitors()
	if err != nil {
		t.Fatalf("ListMonitors() error = %v", err)
	}
	if len(monitors) != 1 {
		t.Fatalf("monitors = %d, want 1", len(monitors))
	}
	m := monitors[0]
	if m.ID != "DP-1" || m.Width != 1920 || m.Height != 1080 {
		t.Fatalf("monitor identity wrong: %+v", m)
	}
	if m.RefreshRate != 60.0 {
		t.Fatalf("refresh_rate = %v, want 60.0 (mHz/1000)", m.RefreshRate)
	}
	if v, _ := m.Metadata["transform"].(int); v != 1 {
		t.Fatalf("transform = %v, want 1 (90 deg)", m.Metadata["transform"])
	}
}

func TestFocusWindowRequest(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) {
		if !strings.Contains(string(req), "FocusWindow") {
			t.Errorf("unexpected request: %s", string(req))
		}
		return nil, nil
	})
	c := f.Client()
	if err := c.FocusWindow("123"); err != nil {
		t.Fatalf("FocusWindow error = %v", err)
	}
	if len(f.requestsSnapshot()) != 1 {
		t.Fatalf("requests = %d", len(f.requestsSnapshot()))
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"FocusWindow":{"id":123}}}`)
}

func TestFocusDirRequest(t *testing.T) {
	tests := []struct {
		dir  string
		want string
	}{
		{"l", "FocusColumnLeft"},
		{"r", "FocusColumnRight"},
		{"u", "FocusWindowUp"},
		{"d", "FocusWindowDown"},
	}
	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
			c := f.Client()
			if err := c.FocusDir(tt.dir); err != nil {
				t.Fatalf("FocusDir(%q) error = %v", tt.dir, err)
			}
			reqs := f.requestsSnapshot()
			if len(reqs) != 1 {
				t.Fatalf("requests = %d", len(reqs))
			}
			jsonEq(t, reqs[0], fmt.Sprintf(`{"Action":%q}`, tt.want))
		})
	}
}

func TestFocusDirInvalid(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.FocusDir("x"); err == nil {
		t.Fatal("expected error for invalid direction")
	}
	if len(f.requestsSnapshot()) != 0 {
		t.Fatalf("no request should be made on invalid input, got %d", len(f.requestsSnapshot()))
	}
}

func TestCloseWindowWithID(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.CloseWindow("5"); err != nil {
		t.Fatalf("CloseWindow error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"CloseWindow":{"id":5}}}`)
}

func TestCloseWindowNoID(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.CloseWindow(""); err != nil {
		t.Fatalf("CloseWindow() error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"CloseWindow":{}}}`)
}

func TestMoveWindowFocusesFirst(t *testing.T) {
	var mu sync.Mutex
	var seen []json.RawMessage
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) {
		mu.Lock()
		seen = append(seen, append(json.RawMessage(nil), req...))
		mu.Unlock()
		return nil, nil
	})
	c := f.Client()
	if err := c.MoveWindow("42", "l"); err != nil {
		t.Fatalf("MoveWindow error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("requests = %d, want 2 (focus then move)", len(seen))
	}
	jsonEq(t, seen[0], `{"Action":{"FocusWindow":{"id":42}}}`)
	jsonEq(t, seen[1], `{"Action":"MoveColumnLeft"}`)
}

func TestMoveWindowNoID(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.MoveWindow("", "r"); err != nil {
		t.Fatalf("MoveWindow() error = %v", err)
	}
	reqs := f.requestsSnapshot()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	jsonEq(t, reqs[0], `{"Action":"MoveColumnRight"}`)
}

func TestMoveWindowInvalidDirection(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.MoveWindow("1", "x"); err == nil {
		t.Fatal("expected error for invalid direction")
	}
	if len(f.requestsSnapshot()) != 0 {
		t.Fatalf("expected no requests on invalid direction, got %d", len(f.requestsSnapshot()))
	}
}

func TestResizeWindowPayload(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.ResizeWindow("7", 800, 600); err != nil {
		t.Fatalf("ResizeWindow error = %v", err)
	}
	reqs := f.requestsSnapshot()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	jsonEq(t, reqs[0], `{"Action":{"SetWindowWidth":{"id":7,"change":{"SetFixed":800}}}}`)
	jsonEq(t, reqs[1], `{"Action":{"SetWindowHeight":{"id":7,"change":{"SetFixed":600}}}}`)
}

func TestResizeWindowNoID(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.ResizeWindow("", 800, 600); err != nil {
		t.Fatalf("ResizeWindow() error = %v", err)
	}
	reqs := f.requestsSnapshot()
	jsonEq(t, reqs[0], `{"Action":{"SetWindowWidth":{"change":{"SetFixed":800}}}}`)
	jsonEq(t, reqs[1], `{"Action":{"SetWindowHeight":{"change":{"SetFixed":600}}}}`)
}

func TestToggleFloatingPayload(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.ToggleFloating("9"); err != nil {
		t.Fatalf("ToggleFloating error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"ToggleWindowFloating":{"id":9}}}`)
}

func TestSetFullscreenUnsupported(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) {
		t.Fatalf("server should not receive request: %s", string(req))
		return nil, nil
	})
	c := f.Client()
	if err := c.SetFullscreen("1", true); err != ipc.ErrNotSupported {
		t.Fatalf("SetFullscreen error = %v, want ErrNotSupported", err)
	}
	if err := c.SetMaximized("1", true); err != ipc.ErrNotSupported {
		t.Fatalf("SetMaximized error = %v, want ErrNotSupported", err)
	}
	if err := c.SetLayout("dwindle"); err != ipc.ErrNotSupported {
		t.Fatalf("SetLayout error = %v, want ErrNotSupported", err)
	}
	if len(f.requestsSnapshot()) != 0 {
		t.Fatalf("no requests should reach server, got %d", len(f.requestsSnapshot()))
	}
}

func TestMoveToMonitorPayload(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.MoveToMonitor("11", "HDMI-A-1"); err != nil {
		t.Fatalf("MoveToMonitor error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"MoveWindowToMonitor":{"id":11,"output":"HDMI-A-1"}}}`)
}

func TestFocusMonitorPayload(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.FocusMonitor("DP-2"); err != nil {
		t.Fatalf("FocusMonitor error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"FocusMonitor":{"output":"DP-2"}}}`)
}

func TestMoveWindowPixelPayload(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.MoveWindowPixel("15", 100, 50); err != nil {
		t.Fatalf("MoveWindowPixel error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"MoveFloatingWindow":{"id":15,"x":{"SetFixed":100},"y":{"SetFixed":50}}}}`)
}

func TestSwitchWorkspaceByID(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.SwitchWorkspace("3"); err != nil {
		t.Fatalf("SwitchWorkspace error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"FocusWorkspace":{"reference":{"Id":3}}}}`)
}

func TestSwitchWorkspaceByName(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.SwitchWorkspace("code"); err != nil {
		t.Fatalf("SwitchWorkspace error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"FocusWorkspace":{"reference":{"Name":"code"}}}}`)
}

func TestMoveToWorkspaceFollows(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.MoveToWorkspace("7", "4"); err != nil {
		t.Fatalf("MoveToWorkspace error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"MoveWindowToWorkspace":{"window_id":7,"reference":{"Id":4},"focus":true}}}`)
}

func TestMoveToWorkspaceSilentNoFollow(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.MoveToWorkspaceSilent("7", "code"); err != nil {
		t.Fatalf("MoveToWorkspaceSilent error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"MoveWindowToWorkspace":{"window_id":7,"reference":{"Name":"code"},"focus":false}}}`)
}

func TestSwitchKeyboardLayoutNext(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.SwitchKeyboardLayout("next"); err != nil {
		t.Fatalf("SwitchKeyboardLayout(next) error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"SwitchLayout":"Next"}}`)
}

func TestSwitchKeyboardLayoutPrev(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.SwitchKeyboardLayout("prev"); err != nil {
		t.Fatalf("SwitchKeyboardLayout(prev) error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"SwitchLayout":"Prev"}}`)
}

func TestSwitchKeyboardLayoutByIndex(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.SwitchKeyboardLayout("2"); err != nil {
		t.Fatalf("SwitchKeyboardLayout(2) error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"SwitchLayout":2}}`)
}

func TestSwitchKeyboardLayoutInvalid(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.SwitchKeyboardLayout("foo"); err == nil {
		t.Fatal("expected error for invalid keyboard layout action")
	}
	if len(f.requestsSnapshot()) != 0 {
		t.Fatalf("no requests should be sent")
	}
}

func TestExecuteUsesShell(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.Execute("kitty"); err != nil {
		t.Fatalf("Execute error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"Spawn":{"command":["sh","-c","kitty"]}}}`)
}

func TestExitHasSkipConfirmation(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.Exit(); err != nil {
		t.Fatalf("Exit error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"Quit":{"skip_confirmation":true}}}`)
}

func TestReloadConfigUsesLoadConfig(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.ReloadConfig(); err != nil {
		t.Fatalf("ReloadConfig error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"LoadConfigFile":{}}}`)
}

func TestLoadConfigWithPath(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.LoadConfig("/tmp/custom.kdl"); err != nil {
		t.Fatalf("LoadConfig error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Action":{"LoadConfigFile":{"path":"/tmp/custom.kdl"}}}`)
}

func TestSetDpmsPayload(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	if err := c.SetDpms("DP-1", true); err != nil {
		t.Fatalf("SetDpms error = %v", err)
	}
	jsonEq(t, f.requestsSnapshot()[0], `{"Output":{"output":"DP-1","action":"On"}}`)
}

func TestUnsupportedOperations(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) {
		t.Fatalf("unexpected request: %s", string(req))
		return nil, nil
	})
	c := f.Client()
	cases := []struct {
		name string
		run  func() error
	}{
		{"PinWindow", func() error { return c.PinWindow("1", true) }},
		{"ToggleGroup", func() error { return c.ToggleGroup("1") }},
		{"GroupNav", func() error { return c.GroupNav("l") }},
		{"SetLayoutProperty", func() error { return c.SetLayoutProperty("1", "k", "v") }},
		{"ToggleSpecialWorkspace", func() error { return c.ToggleSpecialWorkspace("") }},
		{"GetConfig", func() error { _, err := c.GetConfig("x"); return err }},
		{"BatchKeybinds", func() error { return c.BatchKeybinds("{}") }},
		{"RawBatch", func() error { return c.RawBatch("x") }},
		{"GetAnimations", func() error { _, err := c.GetAnimations(); return err }},
		{"GetCursorPosition", func() error { _, _, err := c.GetCursorPosition(); return err }},
		{"BindKey", func() error { return c.BindKey("SUPER", "Return", "exec kitty") }},
		{"UnbindKey", func() error { return c.UnbindKey("SUPER", "Return") }},
		{"SetKeyboardLayouts", func() error { return c.SetKeyboardLayouts("us", "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err != ipc.ErrNotSupported {
				t.Fatalf("%s error = %v, want ErrNotSupported", tc.name, err)
			}
		})
	}
}

func TestSetConfigBorderColorNormalizes(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) {
		t.Fatalf("unexpected request: %s", string(req))
		return nil, nil
	})
	c := f.Client()
	if err := c.SetConfig("border.active_color", "#ff00aa"); err != ipc.ErrNotSupported {
		t.Fatalf("SetConfig error = %v, want ErrNotSupported", err)
	}
}

func TestParseUint64ID(t *testing.T) {
	if _, err := parseUint64ID(""); err == nil {
		t.Fatal("expected error for empty id")
	}
	v, err := parseUint64ID("42")
	if err != nil || v != 42 {
		t.Fatalf("parseUint64ID(42) = %v, %v", v, err)
	}
	if _, err := parseUint64ID("not-a-number"); err == nil {
		t.Fatal("expected error for invalid id")
	}
}

func TestSubscribeStreamBareEvents(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "niri.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	t.Setenv("NIRI_SOCKET", socket)
	c, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		dec := json.NewDecoder(conn)
		var req json.RawMessage
		if err := dec.Decode(&req); err != nil {
			return
		}
		if string(req) != `"EventStream"` {
			t.Errorf("first request = %s, want EventStream", string(req))
		}
		_, _ = conn.Write([]byte(`{"Ok":"Handled"}` + "\n"))
		_, _ = conn.Write([]byte(`{"WorkspacesChanged":{"workspaces":[{"id":1,"idx":1,"output":null,"is_active":true,"is_focused":true,"is_urgent":false,"active_window_id":null,"name":null}]}}` + "\n"))
		_, _ = conn.Write([]byte(`{"WindowOpenedOrChanged":{"window":{"id":7,"title":"hello","app_id":"x","is_focused":true,"is_floating":false,"is_urgent":false,"workspace_id":null,"pid":null,"focus_timestamp":null,"layout":{"pos_in_scrolling_layout":null,"tile_size":[0,0],"window_size":[0,0],"tile_pos_in_workspace_view":null,"window_offset_in_tile":[0,0]}}}}` + "\n"))
		_, _ = conn.Write([]byte(`{"WindowFocusChanged":{"id":7}}` + "\n"))
		_, _ = conn.Write([]byte(`{"WindowClosed":{"id":7}}` + "\n"))
		_, _ = conn.Write([]byte(`{"ConfigLoaded":{"failed":false}}` + "\n"))
	}()

	ch, err := c.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	got := collectEvents(t, ch, 5, 2*time.Second)
	if len(got) < 5 {
		t.Fatalf("got %d events, want at least 5", len(got))
	}

	if got[0].Type != ipc.EventWorkspaceChanged {
		t.Fatalf("event[0] type = %v, want %v", got[0].Type, ipc.EventWorkspaceChanged)
	}
	if got[0].Payload == nil {
		t.Fatalf("event[0] payload is nil")
	}

	if got[1].Type != ipc.EventWindowFocused {
		t.Fatalf("event[1] type = %v, want %v", got[1].Type, ipc.EventWindowFocused)
	}
	if got[1].Window == nil || got[1].Window.ID != "7" || got[1].Window.Title != "hello" {
		t.Fatalf("event[1] window = %+v, want id=7 title=hello", got[1].Window)
	}

	if got[2].Type != ipc.EventWindowFocused {
		t.Fatalf("event[2] type = %v, want %v", got[2].Type, ipc.EventWindowFocused)
	}
	if v, _ := got[2].Payload["id"].(string); v != "7" {
		t.Fatalf("event[2] payload id = %v", got[2].Payload["id"])
	}

	if got[3].Type != ipc.EventWindowClosed {
		t.Fatalf("event[3] type = %v, want %v", got[3].Type, ipc.EventWindowClosed)
	}

	if got[4].Type != ipc.EventConfigReloaded {
		t.Fatalf("event[4] type = %v, want %v", got[4].Type, ipc.EventConfigReloaded)
	}
}

func collectEvents(t *testing.T, ch <-chan ipc.Event, count int, timeout time.Duration) []ipc.Event {
	t.Helper()
	out := make([]ipc.Event, 0, count)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for len(out) < count {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-timer.C:
			return out
		}
	}
	return out
}

func TestSubscribeRejectsErrReply(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) {
		return nil, fmt.Errorf("no events for you")
	})
	c := f.Client()
	ch, err := c.Subscribe()
	if err == nil {
		t.Fatal("Subscribe() expected error, got nil")
	}
	if ch != nil {
		t.Fatal("Subscribe() channel should be nil on error")
	}
}

func TestGetCapabilities(t *testing.T) {
	f := newFakeNiri(t, func(req json.RawMessage) (any, error) { return nil, nil })
	c := f.Client()
	caps, err := c.GetCapabilities()
	if err != nil {
		t.Fatalf("GetCapabilities error = %v", err)
	}
	if !caps.WindowsSupported || !caps.WorkspacesSupported {
		t.Fatalf("GetCapabilities = %+v", caps)
	}
}

func TestSocketPathRespectsAbsolute(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "nested", "niri.sock")
	if err := os.MkdirAll(filepath.Dir(socket), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()
	t.Setenv("NIRI_SOCKET", socket)
	if _, err := New(); err != nil {
		t.Fatalf("New() error = %v", err)
	}
}
