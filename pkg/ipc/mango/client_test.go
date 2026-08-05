package mango

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"axctl/pkg/ipc"
)

func TestLoadConfigEmptyReloads(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) == 2 && parts[0] == "dispatch" && parts[1] == "reload_config" {
			return `{"success":true}`, true, true, ""
		}
		return "", true, false, "unexpected"
	})
	c := s.Client(t)
	if err := c.ReloadConfig(); err != nil {
		t.Fatalf("ReloadConfig error = %v", err)
	}
}

func TestLoadConfigDispatchesPath(t *testing.T) {
	want := "/tmp/generated.conf"
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 3 && parts[0] == "dispatch" && parts[1] == "load_config_file" && parts[2] == want {
			return `{"success":true}`, true, true, ""
		}
		return "", true, false, "unexpected"
	})
	c := s.Client(t)
	if err := c.LoadConfig(want); err != nil {
		t.Fatalf("LoadConfig error = %v", err)
	}
}

func TestNewRequiresSignature(t *testing.T) {
	t.Setenv("MANGO_INSTANCE_SIGNATURE", "")
	if _, err := New(); err == nil {
		t.Fatal("expected error when MANGO_INSTANCE_SIGNATURE is empty")
	} else if !errors.Is(err, ErrNoSignature) {
		t.Fatalf("err = %v, want ErrNoSignature", err)
	}
}

func TestNewValidatesConnection(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "missing.sock")
	t.Setenv("MANGO_INSTANCE_SIGNATURE", socket)
	if _, err := New(); err == nil {
		t.Fatal("expected error for missing socket")
	}
}

func TestNewSucceedsAgainstFake(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 2 && parts[0] == "get" && parts[1] == "cursorpos" {
			return `{"x":0,"y":0}`, false, true, ""
		}
		return "", true, false, "unexpected probe"
	})
	withFakeEnv(t, s.addr())
	if _, err := New(); err != nil {
		t.Fatalf("New() error = %v", err)
	}
}

func TestDialFailure(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "dead.sock")
	if _, err := dialMango(socket, 200*time.Millisecond); err == nil {
		t.Fatal("expected dial error for missing socket")
	}
}

func TestLineDelimitedFraming(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) == 2 && parts[0] == "get" && parts[1] == "all-clients" {
			return mustJSON(t, []map[string]any{
				{"id": float64(1), "title": "a", "appid": "x"},
				{"id": float64(2), "title": "b", "appid": "y", "floating": float64(1)},
			}), false, true, ""
		}
		return "", true, false, "unknown"
	})
	c := s.Client(t)

	var got []mangoClient
	if err := c.conn.Query("get all-clients", &got); err != nil {
		t.Fatalf("Query error = %v", err)
	}
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 2 {
		t.Fatalf("got = %+v", got)
	}
	if got[1].Floating != 1 {
		t.Fatalf("floating = %d, want 1", got[1].Floating)
	}

	reqs := s.Requests()
	if len(reqs) != 1 || reqs[0] != "get all-clients" {
		t.Fatalf("requests = %+v, want [get all-clients]", reqs)
	}
}

func TestDispatchSuccessAndError(t *testing.T) {
	dispatched := map[string]string{}
	mu := &sync.Mutex{}
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 1 && parts[0] == "dispatch" {
			payload := strings.TrimPrefix(strings.Join(parts[1:], " "), "")
			mu.Lock()
			dispatched[payload] = payload
			mu.Unlock()
			if strings.Contains(payload, "killclient") {
				return "", true, false, "no such client"
			}
			return "", true, true, ""
		}
		return "", true, false, "unexpected"
	})
	c := s.Client(t)

	if err := c.conn.Dispatch("togglefloating client,42"); err != nil {
		t.Fatalf("togglefloating error = %v", err)
	}
	if err := c.conn.Dispatch("killclient client,42"); err == nil {
		t.Fatal("expected error from killclient")
	} else if !strings.Contains(err.Error(), "no such client") {
		t.Fatalf("error = %v, want 'no such client'", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if dispatched["togglefloating client,42"] == "" {
		t.Fatalf("togglefloating not dispatched: %+v", dispatched)
	}
	if dispatched["killclient client,42"] == "" {
		t.Fatalf("killclient not dispatched: %+v", dispatched)
	}
}

func TestQueryErrorReply(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 1 && parts[0] == "get" && parts[1] == "missing" {
			return "", true, false, "no such command"
		}
		return "", true, false, "unknown"
	})
	c := s.Client(t)
	err := c.conn.Query("get missing", nil)
	if err == nil || !strings.Contains(err.Error(), "no such command") {
		t.Fatalf("expected error, got %v", err)
	}
}

func TestQueryNullReply(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 2 && parts[0] == "get" && parts[1] == "focusing-client" {
			return `null`, false, true, ""
		}
		return "", true, false, "unexpected"
	})
	c := s.Client(t)
	var raw json.RawMessage
	if err := c.conn.Query("get focusing-client", &raw); err != nil {
		t.Fatalf("Query error = %v", err)
	}
	if string(raw) != "null" {
		t.Fatalf("raw = %s, want null", string(raw))
	}
}

func TestLookLikeReply(t *testing.T) {
	if !looksLikeReply([]byte(`{"success":true}`)) {
		t.Fatal("should detect success-only reply")
	}
	if !looksLikeReply([]byte(`{"error":"boom"}`)) {
		t.Fatal("should detect error-only reply")
	}
	if looksLikeReply([]byte(`[{"id":1}]`)) {
		t.Fatal("should not flag array as reply")
	}
	if looksLikeReply([]byte(`{"id":1,"appid":"a"}`)) {
		t.Fatal("client object should not be flagged as reply")
	}
}

func TestListWindowsParsing(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 2 && parts[0] == "get" && parts[1] == "all-clients" {
			return mustJSON(t, []map[string]any{
				{
					"id":           float64(7),
					"title":        "Firefox",
					"appid":        "firefox",
					"tags":         []float64{1, 0, 0, 0, 0, 0, 0, 0, 0},
					"floating":     float64(0),
					"fullscreen":   float64(1),
					"maximized":    float64(0),
					"global":       float64(0),
					"monitor_name": "DP-1",
					"x":            float64(100),
					"y":            float64(100),
					"width":        float64(800),
					"height":       float64(600),
				},
			}), false, true, ""
		}
		return "", true, false, "unexpected"
	})
	m := s.Client(t)
	wins, err := m.ListWindows()
	if err != nil {
		t.Fatalf("ListWindows error = %v", err)
	}
	if len(wins) != 1 {
		t.Fatalf("windows = %d", len(wins))
	}
	w := wins[0]
	if w.ID != "7" || w.Title != "Firefox" || w.AppID != "firefox" {
		t.Fatalf("identity wrong: %+v", w)
	}
	if w.WorkspaceID != "1" {
		t.Fatalf("workspace id = %q, want 1", w.WorkspaceID)
	}
	if !w.IsFullscreen || w.IsFloating {
		t.Fatalf("flags wrong: fullscreen=%v floating=%v", w.IsFullscreen, w.IsFloating)
	}
	if w.Metadata["monitor"] != "DP-1" {
		t.Fatalf("monitor = %v", w.Metadata["monitor"])
	}
}

func TestActiveWindowNull(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 2 && parts[0] == "get" && parts[1] == "focusing-client" {
			return `null`, false, true, ""
		}
		return "", true, false, "unexpected"
	})
	m := s.Client(t)
	id, err := m.ActiveWindow()
	if err != nil {
		t.Fatalf("ActiveWindow error = %v", err)
	}
	if id != "" {
		t.Fatalf("id = %q, want empty", id)
	}
}

func TestActiveWindowPresent(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 2 && parts[0] == "get" && parts[1] == "focusing-client" {
			return `{"id":99,"title":"x","appid":"y"}`, false, true, ""
		}
		return "", true, false, "unexpected"
	})
	m := s.Client(t)
	id, err := m.ActiveWindow()
	if err != nil {
		t.Fatalf("ActiveWindow error = %v", err)
	}
	if id != "99" {
		t.Fatalf("id = %q, want 99", id)
	}
}

func TestFocusWindowPayload(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 1 && parts[0] == "dispatch" {
			return "", true, true, ""
		}
		return "", true, false, "unexpected"
	})
	m := s.Client(t)
	if err := m.FocusWindow("123"); err != nil {
		t.Fatalf("FocusWindow error = %v", err)
	}
	got := s.Requests()
	if len(got) != 1 {
		t.Fatalf("requests = %d", len(got))
	}
	if got[0] != "dispatch focusid client,123" {
		t.Fatalf("payload = %q, want 'dispatch focusid client,123'", got[0])
	}
}

func TestFocusWindowInvalidID(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		t.Fatalf("unexpected request: %v", parts)
		return "", true, false, "no"
	})
	m := s.Client(t)
	if err := m.FocusWindow("abc"); err == nil {
		t.Fatal("expected error for non-numeric id")
	}
}

func TestCloseWindowPayload(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, true, ""
	})
	m := s.Client(t)
	if err := m.CloseWindow("5"); err != nil {
		t.Fatalf("CloseWindow error = %v", err)
	}
	if s.Requests()[0] != "dispatch killclient client,5" {
		t.Fatalf("payload = %q", s.Requests()[0])
	}
}

func TestMoveWindowPayload(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, true, ""
	})
	m := s.Client(t)
	if err := m.MoveWindow("9", "l"); err != nil {
		t.Fatalf("MoveWindow error = %v", err)
	}
	if s.Requests()[0] != "dispatch smartmovewin client,9 left" {
		t.Fatalf("payload = %q", s.Requests()[0])
	}
}

func TestMoveWindowNormalizesDirection(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, true, ""
	})
	m := s.Client(t)
	if err := m.MoveWindow("1", "r"); err != nil {
		t.Fatal(err)
	}
	if s.Requests()[0] != "dispatch smartmovewin client,1 right" {
		t.Fatalf("payload = %q", s.Requests()[0])
	}
}

func TestResizeWindowPayload(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, true, ""
	})
	m := s.Client(t)
	if err := m.ResizeWindow("3", 1200, 800); err != nil {
		t.Fatal(err)
	}
	if s.Requests()[0] != "dispatch resizewin client,3 1200 800" {
		t.Fatalf("payload = %q", s.Requests()[0])
	}
}

func TestToggleFloatingPayload(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, true, ""
	})
	m := s.Client(t)
	if err := m.ToggleFloating("77"); err != nil {
		t.Fatal(err)
	}
	if s.Requests()[0] != "dispatch togglefloating client,77" {
		t.Fatalf("payload = %q", s.Requests()[0])
	}
}

func TestSetFullscreenSkipsWhenSame(t *testing.T) {
	queries := 0
	dispatches := 0
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		switch {
		case len(parts) >= 3 && parts[0] == "get" && parts[1] == "client" && parts[2] == "42":
			queries++
			return `{"id":42,"title":"x","appid":"y","fullscreen":1}`, false, true, ""
		case len(parts) >= 1 && parts[0] == "dispatch":
			dispatches++
			return "", true, true, ""
		}
		return "", true, false, "unexpected"
	})
	m := s.Client(t)
	if err := m.SetFullscreen("42", true); err != nil {
		t.Fatalf("error = %v", err)
	}
	if queries != 1 || dispatches != 0 {
		t.Fatalf("queries=%d dispatches=%d, want 1/0", queries, dispatches)
	}
}

func TestSetFullscreenDispatchesWhenDiffers(t *testing.T) {
	dispatches := 0
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		switch {
		case len(parts) >= 3 && parts[0] == "get" && parts[1] == "client":
			return `{"id":1,"title":"x","appid":"y","fullscreen":0}`, false, true, ""
		case len(parts) >= 1 && parts[0] == "dispatch":
			dispatches++
			return "", true, true, ""
		}
		return "", true, false, "unexpected"
	})
	m := s.Client(t)
	if err := m.SetFullscreen("1", true); err != nil {
		t.Fatal(err)
	}
	if dispatches != 1 {
		t.Fatalf("dispatches = %d, want 1", dispatches)
	}
	reqs := s.Requests()
	for _, r := range reqs {
		if r == "dispatch togglefullscreen client,1" {
			return
		}
	}
	t.Fatalf("expected togglefullscreen dispatch in %v", reqs)
}

func TestSetFullscreenInvalidID(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		t.Fatalf("unexpected: %v", parts)
		return "", false, false, ""
	})
	m := s.Client(t)
	if err := m.SetFullscreen("abc", true); err == nil {
		t.Fatal("expected error for invalid id")
	}
}

func TestSetMaximizedSkipsWhenSame(t *testing.T) {
	dispatches := 0
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		switch {
		case len(parts) >= 3 && parts[0] == "get" && parts[1] == "client":
			return `{"id":1,"title":"x","appid":"y","fullscreen":0,"maximized":1}`, false, true, ""
		case len(parts) >= 1 && parts[0] == "dispatch":
			dispatches++
			return "", true, true, ""
		}
		return "", true, false, ""
	})
	m := s.Client(t)
	if err := m.SetMaximized("1", true); err != nil {
		t.Fatal(err)
	}
	if dispatches != 0 {
		t.Fatalf("dispatches = %d, want 0", dispatches)
	}
}

func TestSetMaximizedDispatchesWhenDiffers(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		switch {
		case len(parts) >= 3 && parts[0] == "get" && parts[1] == "client":
			return `{"id":1,"title":"x","appid":"y","maximized":0,"global":0}`, false, true, ""
		case len(parts) >= 1 && parts[0] == "dispatch":
			return "", true, true, ""
		}
		return "", true, false, ""
	})
	m := s.Client(t)
	if err := m.SetMaximized("1", true); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range s.Requests() {
		if r == "dispatch togglemaximizescreen client,1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected togglemaximizescreen, got %v", s.Requests())
	}
}

func TestPinWindowDispatchesWhenDiffers(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		switch {
		case len(parts) >= 3 && parts[0] == "get" && parts[1] == "client":
			return `{"id":1,"title":"x","appid":"y","global":0}`, false, true, ""
		case len(parts) >= 1 && parts[0] == "dispatch":
			return "", true, true, ""
		}
		return "", true, false, ""
	})
	m := s.Client(t)
	if err := m.PinWindow("1", true); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range s.Requests() {
		if r == "dispatch toggleglobal client,1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected toggleglobal, got %v", s.Requests())
	}
}

func TestPinWindowSkipsWhenSame(t *testing.T) {
	dispatches := 0
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		switch {
		case len(parts) >= 3 && parts[0] == "get" && parts[1] == "client":
			return `{"id":1,"title":"x","appid":"y","global":1}`, false, true, ""
		case len(parts) >= 1 && parts[0] == "dispatch":
			dispatches++
			return "", true, true, ""
		}
		return "", true, false, ""
	})
	m := s.Client(t)
	if err := m.PinWindow("1", true); err != nil {
		t.Fatal(err)
	}
	if dispatches != 0 {
		t.Fatalf("dispatches = %d, want 0", dispatches)
	}
}

func TestMoveWindowPixelPayload(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, true, ""
	})
	m := s.Client(t)
	if err := m.MoveWindowPixel("10", 100, 200); err != nil {
		t.Fatal(err)
	}
	if s.Requests()[0] != "dispatch movewin client,10 100 200" {
		t.Fatalf("payload = %q", s.Requests()[0])
	}
}

func TestSwitchWorkspacePayload(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, true, ""
	})
	m := s.Client(t)
	if err := m.SwitchWorkspace("3"); err != nil {
		t.Fatal(err)
	}
	if s.Requests()[0] != "dispatch view 3" {
		t.Fatalf("payload = %q", s.Requests()[0])
	}
}

func TestMoveToWorkspacePayload(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, true, ""
	})
	m := s.Client(t)
	if err := m.MoveToWorkspace("7", "4"); err != nil {
		t.Fatal(err)
	}
	if s.Requests()[0] != "dispatch tag client,7 4" {
		t.Fatalf("payload = %q", s.Requests()[0])
	}
}

func TestMoveToWorkspaceSilentUsesTagsilent(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, true, ""
	})
	m := s.Client(t)
	if err := m.MoveToWorkspaceSilent("7", "4"); err != nil {
		t.Fatal(err)
	}
	if s.Requests()[0] != "dispatch tagsilent client,7 4" {
		t.Fatalf("payload = %q", s.Requests()[0])
	}
}

func TestFocusMonitorPayload(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, true, ""
	})
	m := s.Client(t)
	if err := m.FocusMonitor("DP-2"); err != nil {
		t.Fatal(err)
	}
	if s.Requests()[0] != "dispatch focusmon DP-2" {
		t.Fatalf("payload = %q", s.Requests()[0])
	}
}

func TestMoveToMonitorPayload(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, true, ""
	})
	m := s.Client(t)
	if err := m.MoveToMonitor("11", "HDMI-A-1"); err != nil {
		t.Fatal(err)
	}
	if s.Requests()[0] != "dispatch tagmon client,11 HDMI-A-1" {
		t.Fatalf("payload = %q", s.Requests()[0])
	}
}

func TestSetDpmsSleepAndWake(t *testing.T) {
	for _, tc := range []struct {
		name   string
		on     bool
		expect string
	}{
		{"off", false, "dispatch sleep_monitor DP-1"},
		{"on", true, "dispatch wakeup_monitor DP-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
				return "", true, true, ""
			})
			m := s.Client(t)
			if err := m.SetDpms("DP-1", tc.on); err != nil {
				t.Fatal(err)
			}
			if s.Requests()[0] != tc.expect {
				t.Fatalf("got %q, want %q", s.Requests()[0], tc.expect)
			}
		})
	}
}

func TestSetLayoutPayload(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, true, ""
	})
	m := s.Client(t)
	if err := m.SetLayout("tile"); err != nil {
		t.Fatal(err)
	}
	if s.Requests()[0] != "dispatch setlayout tile" {
		t.Fatalf("payload = %q", s.Requests()[0])
	}
}

func TestExecutePayload(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, true, ""
	})
	m := s.Client(t)
	if err := m.Execute("kitty"); err != nil {
		t.Fatal(err)
	}
	if s.Requests()[0] != "dispatch spawn_shell kitty" {
		t.Fatalf("payload = %q", s.Requests()[0])
	}
}

func TestExitPayload(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, true, ""
	})
	m := s.Client(t)
	if err := m.Exit(); err != nil {
		t.Fatal(err)
	}
	if s.Requests()[0] != "dispatch quit" {
		t.Fatalf("payload = %q", s.Requests()[0])
	}
}

func TestGetCursorPosition(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 2 && parts[0] == "get" && parts[1] == "cursorpos" {
			return `{"x":640,"y":480}`, false, true, ""
		}
		return "", true, false, "unexpected"
	})
	m := s.Client(t)
	x, y, err := m.GetCursorPosition()
	if err != nil {
		t.Fatal(err)
	}
	if x != 640 || y != 480 {
		t.Fatalf("cursor = (%d, %d), want (640, 480)", x, y)
	}
}

func TestListMonitorsParsing(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 2 && parts[0] == "get" && parts[1] == "all-monitors" {
			return mustJSON(t, []map[string]any{
				{"name": "DP-1", "active": float64(1), "focused": float64(1), "x": float64(0), "y": float64(0), "width": float64(1920), "height": float64(1080), "scale": float64(100), "index": float64(0)},
				{"name": "HDMI-A-1", "active": float64(0), "focused": float64(0), "x": float64(1920), "y": float64(0), "width": float64(2560), "height": float64(1440), "scale": float64(100), "index": float64(1)},
			}), false, true, ""
		}
		return "", true, false, "unexpected"
	})
	m := s.Client(t)
	monitors, err := m.ListMonitors()
	if err != nil {
		t.Fatalf("ListMonitors error = %v", err)
	}
	if len(monitors) != 2 {
		t.Fatalf("monitors = %d, want 2", len(monitors))
	}
	if monitors[0].Name != "DP-1" || monitors[1].Name != "HDMI-A-1" {
		t.Fatalf("names = %v", monitors)
	}
	if monitors[0].Scale != 1.0 {
		t.Fatalf("scale = %v, want 1.0", monitors[0].Scale)
	}
	if !monitors[0].IsFocused {
		t.Fatalf("monitor 0 should be focused")
	}
}

func TestListWorkspacesParsing(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 2 && parts[0] == "get" && parts[1] == "all-tags" {
			return mustJSON(t, []map[string]any{
				{"name": "1", "index": float64(0), "active": float64(1), "urgent": float64(0)},
				{"name": "2", "index": float64(1), "active": float64(0), "urgent": float64(0)},
				{"name": "3", "index": float64(2), "active": float64(0), "urgent": float64(1)},
			}), false, true, ""
		}
		return "", true, false, "unexpected"
	})
	m := s.Client(t)
	wss, err := m.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces error = %v", err)
	}
	if len(wss) != 3 {
		t.Fatalf("workspaces = %d", len(wss))
	}
	if wss[0].Name != "1" || !wss[0].IsActive {
		t.Fatalf("active workspace wrong: %+v", wss[0])
	}
	if meta := wss[2].Metadata["urgent"]; meta != true {
		t.Fatalf("urgent = %v, want true", meta)
	}
}

func TestActiveWorkspace(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 2 && parts[0] == "get" && parts[1] == "all-tags" {
			return mustJSON(t, []map[string]any{
				{"name": "1", "index": float64(0), "active": float64(0)},
				{"name": "5", "index": float64(4), "active": float64(1)},
			}), false, true, ""
		}
		return "", true, false, ""
	})
	m := s.Client(t)
	ws, err := m.ActiveWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	if ws.Name != "5" {
		t.Fatalf("active = %+v", ws)
	}
}

func TestCapabilities(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, false, "no requests expected"
	})
	m := s.Client(t)
	caps, err := m.GetCapabilities()
	if err != nil {
		t.Fatal(err)
	}
	if !caps.WindowsSupported || !caps.WorkspacesSupported {
		t.Fatalf("expected windows/workspaces true: %+v", caps)
	}
	if caps.Blur || caps.Shadows || caps.Animations || caps.RoundedCorners {
		t.Fatalf("expected effects false: %+v", caps)
	}
	if len(s.Requests()) != 0 {
		t.Fatalf("no requests expected, got %v", s.Requests())
	}
}

func TestUnsupportedMethods(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		t.Fatalf("unexpected request: %v", parts)
		return "", true, false, ""
	})
	m := s.Client(t)
	cases := []struct {
		name string
		fn   func() error
	}{
		{"ToggleGroup", func() error { return m.ToggleGroup("1") }},
		{"GroupNav", func() error { return m.GroupNav("l") }},
		{"SetLayoutProperty", func() error { return m.SetLayoutProperty("1", "k", "v") }},
		{"BatchKeybinds", func() error { return m.BatchKeybinds("{}") }},
		{"RawBatch", func() error { return m.RawBatch("x") }},
		{"GetAnimations", func() error { _, err := m.GetAnimations(); return err }},
		{"BindKey", func() error { return m.BindKey("SUPER", "Return", "x") }},
		{"UnbindKey", func() error { return m.UnbindKey("SUPER", "Return") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if err == nil {
				t.Fatal("expected error")
			}
			if err != ipc.ErrNotSupported && err.Error() != ipc.ErrNotSupported.Error() {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestReloadConfigDispatches(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 1 && parts[0] == "dispatch" {
			return "", true, true, ""
		}
		return "", true, false, ""
	})
	m := s.Client(t)
	if err := m.ReloadConfig(); err != nil {
		t.Fatal(err)
	}
	if s.Requests()[0] != "dispatch reload_config" {
		t.Fatalf("payload = %q", s.Requests()[0])
	}
}

func TestParseClientID(t *testing.T) {
	if _, err := parseClientID(""); err == nil {
		t.Fatal("expected error for empty")
	}
	if _, err := parseClientID("abc"); err == nil {
		t.Fatal("expected error for non-numeric")
	}
	v, err := parseClientID("42")
	if err != nil || v != 42 {
		t.Fatalf("parseClientID(42) = %d, %v", v, err)
	}
}

func TestClientTargetEnvelope(t *testing.T) {
	got, err := clientTarget("123")
	if err != nil {
		t.Fatal(err)
	}
	if got != "client,123" {
		t.Fatalf("clientTarget = %q", got)
	}
	if _, err := clientTarget(""); err == nil {
		t.Fatal("expected error for empty id")
	}
	if _, err := clientTarget("notnum"); err == nil {
		t.Fatal("expected error for invalid id")
	}
}

func TestSubscribeClosesCleanly(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "mango.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	subscribeAccept := make(chan struct{})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			if subscribeAccept != nil {
				select {
				case <-subscribeAccept:
				default:
					close(subscribeAccept)
				}
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufioReader(c)
				line, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if !strings.HasPrefix(line, "watch ") {
					return
				}
				_, _ = c.Write([]byte(`[{"id":1,"title":"a","appid":"x"}]` + "\n"))
				// Wait for client close
				buf := make([]byte, 1)
				for {
					if _, err := c.Read(buf); err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	ws, err := openWatches(socket, []string{"all-clients"})
	if err != nil {
		t.Fatalf("openWatches: %v", err)
	}

	select {
	case ev, ok := <-ws.Updates():
		if !ok {
			t.Fatal("updates closed unexpectedly before data")
		}
		if ev.Type != ipc.EventWindowCreated {
			t.Fatalf("event type = %v, want WindowCreated", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for update")
	}

	ws.Close()
	select {
	case _, ok := <-ws.Updates():
		if ok {
			t.Fatal("expected updates channel to close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for close")
	}
}

func TestSubscribeDialFailure(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "missing.sock")
	_, err := openWatches(socket, []string{"all-clients"})
	if err == nil {
		t.Fatal("expected dial error")
	}
}

func TestSubscribeMapsUpdateLines(t *testing.T) {
	cases := []struct {
		line string
		want ipc.EventType
	}{
		{`[{"id":1,"title":"a","appid":"x","monitor_name":"DP-1"}]`, ipc.EventWindowCreated},
		{`{"id":2,"title":"b","appid":"y","monitor_name":"DP-1"}`, ipc.EventWindowFocused},
		{`[{"name":"DP-1","active":1,"width":1920,"height":1080}]`, ipc.EventMonitorChanged},
		{`[{"name":"1","index":0,"active":1,"urgent":0}]`, ipc.EventWorkspaceChanged},
		{`{"x":100,"y":200}`, ipc.EventWorkspaceChanged},
	}
	for _, tc := range cases {
		t.Run(string(tc.want), func(t *testing.T) {
			ws := &watchSession{done: make(chan struct{})}
			ev := ws.mapLine([]byte(tc.line))
			if ev.Type != tc.want {
				t.Fatalf("event type = %v, want %v", ev.Type, tc.want)
			}
			close(ws.done)
		})
	}
}

func TestSubscribeViaClient(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "mango.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufioReader(c)
				line, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if !strings.HasPrefix(line, "watch ") {
					_, _ = c.Write([]byte(`{"success":true}` + "\n"))
					return
				}
				_, _ = c.Write([]byte(`[{"id":99,"title":"focus","appid":"a"}]` + "\n"))
				buf := make([]byte, 1)
				for {
					if _, err := c.Read(buf); err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	m := &Mango{conn: &mmsgConn{}, socketPath: socket}
	ch, err := m.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe error = %v", err)
	}
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("channel closed early")
		}
		if ev.Type != ipc.EventWindowCreated {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}

	m.mu.Lock()
	m.subscribed = true
	m.mu.Unlock()
	if _, err := m.Subscribe(); err != nil {
		t.Fatalf("second subscribe should not error: %v", err)
	}
}

func TestDispatchErrorContainsReason(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, false, "permission denied"
	})
	c := s.Client(t)
	err := c.conn.Dispatch("killclient client,1")
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v", err)
	}
}

func TestRawJSONLineRecognizedAsClient(t *testing.T) {
	clientJSON := `{"id":15,"title":"calc","appid":"qalc","monitor_name":"DP-1","fullscreen":1}`
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 2 && parts[0] == "get" && parts[1] == "focusing-client" {
			return clientJSON, false, true, ""
		}
		return "", true, false, ""
	})
	c := s.Client(t)
	var raw mangoClient
	if err := c.conn.Query("get focusing-client", &raw); err != nil {
		t.Fatal(err)
	}
	if raw.ID != 15 || raw.Title != "calc" || !raw.isFullscreen() {
		t.Fatalf("parse wrong: %+v", raw)
	}
	ws := &watchSession{done: make(chan struct{})}
	defer close(ws.done)
	ev := ws.mapLine([]byte(clientJSON))
	if ev.Type != ipc.EventWindowFocused {
		t.Fatalf("event type = %v", ev.Type)
	}
	if ev.Window == nil || ev.Window.ID != "15" {
		t.Fatalf("event window = %+v", ev.Window)
	}
}

func (c *mangoClient) isFullscreen() bool { return c.Fullscreen != 0 }

func TestManyRequestsSerial(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, true, ""
	})
	m := s.Client(t)
	for i := 0; i < 25; i++ {
		if err := m.conn.Dispatch(fmt.Sprintf("noop %d", i)); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}
	if got := len(s.Requests()); got != 25 {
		t.Fatalf("requests = %d, want 25", got)
	}
}

func TestSocketPathAbsolute(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "mango.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("MANGO_INSTANCE_SIGNATURE", socket)
	got, err := resolveSocketPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != socket {
		t.Fatalf("resolve = %q, want %q", got, socket)
	}
}

func TestSocketPathDerived(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/tmp/xdg-runtime")
	t.Setenv("MANGO_INSTANCE_SIGNATURE", "abc123")
	got, err := resolveSocketPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/tmp/xdg-runtime", "mango-ipc-abc123")
	if got != want {
		t.Fatalf("resolve = %q, want %q", got, want)
	}
}

func bufioReader(c net.Conn) *bufio.Reader { return bufio.NewReader(c) }

func TestWriteReplyUnwrapped(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return `{"foo":1}`, false, true, ""
	})
	c := s.Client(t)
	var got map[string]int
	if err := c.conn.Query("hi", &got); err != nil {
		t.Fatal(err)
	}
	if got["foo"] != 1 {
		t.Fatalf("got = %+v", got)
	}
}

func TestEnvIsolation(t *testing.T) {
	old, hadOld := os.LookupEnv("MANGO_INSTANCE_SIGNATURE")
	defer func() {
		if hadOld {
			_ = os.Setenv("MANGO_INSTANCE_SIGNATURE", old)
		} else {
			_ = os.Unsetenv("MANGO_INSTANCE_SIGNATURE")
		}
	}()
	dir := t.TempDir()
	socket := filepath.Join(dir, "mango.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	t.Setenv("MANGO_INSTANCE_SIGNATURE", socket)
	if got := os.Getenv("MANGO_INSTANCE_SIGNATURE"); got != socket {
		t.Fatalf("sig env = %q", got)
	}
}

func TestReadJSONHelpers(t *testing.T) {
	lines := []string{`{"id":1}`, `{"success":true}`, `{"foo":1}`}
	for _, l := range lines {
		if !json.Valid([]byte(l)) {
			t.Fatalf("invalid JSON: %q", l)
		}
	}
}

func TestAllListsAndDispatch(t *testing.T) {
	calls := map[string]int{}
	var mu sync.Mutex
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		mu.Lock()
		calls[strings.Join(parts[:1], " ")]++
		mu.Unlock()
		if len(parts) >= 2 && parts[0] == "get" && parts[1] == "all-clients" {
			return "[]", false, true, ""
		}
		if len(parts) >= 2 && parts[0] == "get" && parts[1] == "all-monitors" {
			return "[]", false, true, ""
		}
		if len(parts) >= 2 && parts[0] == "get" && parts[1] == "all-tags" {
			return "[]", false, true, ""
		}
		return "", true, true, ""
	})
	m := s.Client(t)
	if _, err := m.ListWindows(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ListMonitors(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ListWorkspaces(); err != nil {
		t.Fatal(err)
	}
	if err := m.Exit(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["get"] != 3 {
		t.Fatalf("get calls = %d, want 3", calls["get"])
	}
	if calls["dispatch"] != 1 {
		t.Fatalf("dispatch calls = %d, want 1", calls["dispatch"])
	}
}

func TestReloadError(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		return "", true, false, "reload failed"
	})
	m := s.Client(t)
	if err := m.ReloadConfig(); err == nil || !strings.Contains(err.Error(), "reload failed") {
		t.Fatalf("expected error, got %v", err)
	}
}

func TestSetConfigUnsupported(t *testing.T) {
	s := newFakeServer(t, func(t *testing.T, parts []string) (string, bool, bool, string) {
		t.Fatalf("unexpected: %v", parts)
		return "", true, false, ""
	})
	m := s.Client(t)
	if err := m.SetConfig("nonexistent", 1); err != ipc.ErrNotSupported {
		t.Fatalf("err = %v, want ErrNotSupported", err)
	}
}

func writeStringTo(t *testing.T) {}
