package niri

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// startFakeNiri starts a fake niri IPC server on a Unix socket that responds
// to the documented niri protocol: {"Windows":null} -> {"Ok":{"Windows":[...]}}.
// It returns the socket path and a cleanup func.
func startFakeNiri(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "niri.sock")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeConn(conn)
		}
	}()

	return sock, func() { ln.Close() }
}

func handleFakeConn(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	for {
		var req map[string]json.RawMessage
		if err := dec.Decode(&req); err != nil {
			return
		}
		// Query requests: {"Windows":null}, {"Workspaces":null}, {"Outputs":null}
		if _, ok := req["Windows"]; ok {
			enc.Encode(map[string]interface{}{
				"Ok": map[string]interface{}{
					"Windows": []map[string]interface{}{
						{"id": 1, "title": "foot", "app_id": "foot", "workspace_id": 1, "is_focused": true},
					},
				},
			})
			continue
		}
		if _, ok := req["Workspaces"]; ok {
			enc.Encode(map[string]interface{}{
				"Ok": map[string]interface{}{
					"Workspaces": []map[string]interface{}{
						{"id": 1, "idx": 1, "name": "main", "output": "eDP-1", "is_active": true, "is_focused": true},
					},
				},
			})
			continue
		}
		if _, ok := req["Outputs"]; ok {
			enc.Encode(map[string]interface{}{
				"Ok": map[string]interface{}{
					"Outputs": map[string]interface{}{
						"eDP-1": map[string]interface{}{
							"name": "eDP-1", "make": "Apple", "model": "LCD",
							"logical": map[string]interface{}{"x": 0, "y": 0, "width": 1920, "height": 1200, "scale": 1.5, "transform": "Normal"},
						},
					},
				},
			})
			continue
		}
		if _, ok := req["FocusedWindow"]; ok {
			enc.Encode(map[string]interface{}{
				"Ok": map[string]interface{}{"FocusedWindow": map[string]interface{}{"id": 1}},
			})
			continue
		}
		// Actions: {"Action":{...}} -> {"Ok":"Handled"}
		if _, ok := req["Action"]; ok {
			enc.Encode(map[string]interface{}{"Ok": "Handled"})
			continue
		}
		// Unknown -> error
		enc.Encode(map[string]interface{}{"Err": "unknown request"})
	}
}

func newTestNiri(t *testing.T) (*Niri, func()) {
	t.Helper()
	sock, cleanup := startFakeNiri(t)
	home := t.TempDir()
	os.Setenv("HOME", home)
	os.Setenv("NIRI_SOCKET", sock)
	n, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return n, cleanup
}

func TestListWindows(t *testing.T) {
	n, cleanup := newTestNiri(t)
	defer cleanup()

	wins, err := n.ListWindows()
	if err != nil {
		t.Fatalf("ListWindows: %v", err)
	}
	if len(wins) != 1 {
		t.Fatalf("expected 1 window, got %d", len(wins))
	}
	if wins[0].AppID != "foot" {
		t.Fatalf("expected app_id foot, got %q", wins[0].AppID)
	}
	if wins[0].WorkspaceID != "1" {
		t.Fatalf("expected workspace 1, got %q", wins[0].WorkspaceID)
	}
}

func TestListWorkspaces(t *testing.T) {
	n, cleanup := newTestNiri(t)
	defer cleanup()

	wss, err := n.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(wss) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(wss))
	}
	if wss[0].Name != "main" {
		t.Fatalf("expected name main, got %q", wss[0].Name)
	}
}

func TestListMonitors(t *testing.T) {
	n, cleanup := newTestNiri(t)
	defer cleanup()

	mons, err := n.ListMonitors()
	if err != nil {
		t.Fatalf("ListMonitors: %v", err)
	}
	if len(mons) != 1 {
		t.Fatalf("expected 1 monitor, got %d", len(mons))
	}
	if mons[0].Name != "eDP-1" {
		t.Fatalf("expected eDP-1, got %q", mons[0].Name)
	}
	if mons[0].Width != 1920 {
		t.Fatalf("expected width 1920, got %d", mons[0].Width)
	}
	if !mons[0].IsFocused {
		t.Fatal("expected eDP-1 to be focused (derived from focused workspace)")
	}
	if mons[0].Metadata["active_workspace"] != "1" {
		t.Fatalf("expected active_workspace=1, got %v", mons[0].Metadata["active_workspace"])
	}
}

func TestActiveWindow(t *testing.T) {
	n, cleanup := newTestNiri(t)
	defer cleanup()

	id, err := n.ActiveWindow()
	if err != nil {
		t.Fatalf("ActiveWindow: %v", err)
	}
	if id != "1" {
		t.Fatalf("expected id 1, got %q", id)
	}
}

func TestGetCapabilitiesShadowsFalse(t *testing.T) {
	n, cleanup := newTestNiri(t)
	defer cleanup()

	caps, err := n.GetCapabilities()
	if err != nil {
		t.Fatalf("GetCapabilities: %v", err)
	}
	if caps.Shadows {
		t.Fatal("expected Shadows=false for niri (niri does not render shadows)")
	}
	if !caps.Blur {
		t.Fatal("expected Blur=true for niri")
	}
}

func TestBatchKeybindsWritesFile(t *testing.T) {
	n, cleanup := newTestNiri(t)
	defer cleanup()

	payload := `{"binds":[{"modifiers":["SUPER"],"key":"T","dispatcher":"exec","argument":"foot","enabled":true}],"unbinds":[]}`
	if err := n.BatchKeybinds(payload); err != nil {
		t.Fatalf("BatchKeybinds: %v", err)
	}

	data, err := os.ReadFile(n.ambxstConfigPath)
	if err != nil {
		t.Fatalf("read ambxst.kdl: %v", err)
	}
	content := string(data)
	if !contains(content, "Mod+T") {
		t.Fatalf("expected Mod+T in generated file, got:\n%s", content)
	}
	if !contains(content, `spawn "foot"`) {
		t.Fatalf("expected spawn foot in generated file, got:\n%s", content)
	}
}

func TestSetConfigWritesAppearance(t *testing.T) {
	n, cleanup := newTestNiri(t)
	defer cleanup()

	if err := n.SetConfig("gaps.inner", 9); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	appearancePath := filepath.Join(filepath.Dir(n.ambxstConfigPath), "ambxst-appearance.kdl")
	data, err := os.ReadFile(appearancePath)
	if err != nil {
		t.Fatalf("read appearance: %v", err)
	}
	if !contains(string(data), "gaps 9") {
		t.Fatalf("expected gaps 9 in appearance, got:\n%s", string(data))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
