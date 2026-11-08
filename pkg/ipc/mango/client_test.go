package mango

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"axctl/pkg/ipc"
)

func TestNewRequiresSignature(t *testing.T) {
	t.Setenv("MANGO_INSTANCE_SIGNATURE", "")
	t.Setenv("XDG_CURRENT_DESKTOP", "")
	if _, err := New(); !errors.Is(err, ErrNoSignature) {
		t.Fatalf("New() error = %v, want ErrNoSignature", err)
	}
}

func TestResolveSocketPathDiscoversMangoSocket(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "mango-42.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("MANGO_INSTANCE_SIGNATURE", "")
	t.Setenv("XDG_CURRENT_DESKTOP", "mango")
	t.Setenv("XDG_RUNTIME_DIR", dir)
	got, err := resolveSocketPath()
	if err != nil || got != socket {
		t.Fatalf("resolveSocketPath() = %q, %v; want %q", got, err, socket)
	}
}

func TestSocketPathUsesSignatureAsSocketPath(t *testing.T) {
	t.Setenv("MANGO_INSTANCE_SIGNATURE", "/tmp/mango-42.sock")
	got, err := resolveSocketPath()
	if err != nil || got != "/tmp/mango-42.sock" {
		t.Fatalf("resolveSocketPath() = %q, %v", got, err)
	}
	t.Setenv("XDG_RUNTIME_DIR", "/tmp/xdg")
	t.Setenv("MANGO_INSTANCE_SIGNATURE", "mango-42.sock")
	got, err = resolveSocketPath()
	want := filepath.Join("/tmp/xdg", "mango-42.sock")
	if err != nil || got != want {
		t.Fatalf("resolveSocketPath() = %q, %v, want %q", got, err, want)
	}
}

func TestNewValidatesMangoSocket(t *testing.T) {
	s := newFakeServer(t, func(_ *testing.T, parts []string) (string, bool, bool, string) {
		if strings.Join(parts, " ") == "get cursorpos" {
			return `{"x":1,"y":2}`, false, true, ""
		}
		return "", true, false, "unexpected request"
	})
	withFakeEnv(t, s.addr())
	m, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if m.SocketPath() != s.addr() {
		t.Fatalf("SocketPath() = %q, want %q", m.SocketPath(), s.addr())
	}
	if got := s.Requests(); len(got) != 1 || got[0] != "get cursorpos" {
		t.Fatalf("requests = %v", got)
	}
}

func TestMmsgQueryAndDispatchUseOneRequestPerConnection(t *testing.T) {
	s := newFakeServer(t, func(_ *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 2 && parts[0] == "get" && parts[1] == "all-clients" {
			return `{"clients":[{"id":1,"title":"Terminal","appid":"foot"}]}`, false, true, ""
		}
		if len(parts) > 0 && parts[0] == "dispatch" {
			return "", true, true, ""
		}
		return "", true, false, "unexpected request"
	})
	m := s.Client(t)
	windows, err := m.ListWindows()
	if err != nil || len(windows) != 1 || windows[0].ID != "1" {
		t.Fatalf("ListWindows() = %+v, %v", windows, err)
	}
	if err := m.FocusWindow("1"); err != nil {
		t.Fatal(err)
	}
	got := s.Requests()
	if len(got) != 2 || got[0] != "get all-clients" || got[1] != "dispatch focusid client,1" {
		t.Fatalf("requests = %v", got)
	}
}

func TestDispatchErrorIncludesMangoMessage(t *testing.T) {
	s := newFakeServer(t, func(_ *testing.T, _ []string) (string, bool, bool, string) {
		return "", true, false, "no such client"
	})
	err := s.Client(t).CloseWindow("7")
	if err == nil || !strings.Contains(err.Error(), "no such client") {
		t.Fatalf("CloseWindow error = %v", err)
	}
}

func TestListWindowsMapsMangoClientFields(t *testing.T) {
	s := newFakeServer(t, func(_ *testing.T, parts []string) (string, bool, bool, string) {
		if strings.Join(parts, " ") == "get all-clients" {
			return `{"clients":[{"id":7,"pid":72,"foreign_toplevel_id":"ft-7","title":"Firefox","appid":"firefox","monitor":"DP-1","tags":[1,3],"is_xwayland":true,"is_visible":true,"is_focused":true,"is_fullscreen":true,"is_floating":true,"is_maximized":false,"is_global":true,"is_fakefullscreen":true,"x":100,"y":200,"width":800,"height":600,"scroller_proportion":0.8}]}`, false, true, ""
		}
		return "", true, false, "unexpected request"
	})
	windows, err := s.Client(t).ListWindows()
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) != 1 {
		t.Fatalf("windows = %+v", windows)
	}
	w := windows[0]
	if w.ID != "7" || w.WorkspaceID != "DP-1:1" || !w.IsFloating || !w.IsFullscreen || !w.IsFocused {
		t.Fatalf("window normalization = %+v", w)
	}
	if w.Metadata["monitor_id"] != "DP-1" || w.Metadata["pid"] != 72 {
		t.Fatalf("metadata = %+v", w.Metadata)
	}
	if tags, ok := w.Metadata["tags"].([]int); !ok || len(tags) != 2 || tags[1] != 3 {
		t.Fatalf("tags metadata = %#v", w.Metadata["tags"])
	}
}

func TestActiveWindowHandlesNoFocusedClient(t *testing.T) {
	s := newFakeServer(t, func(_ *testing.T, _ []string) (string, bool, bool, string) {
		return "", true, false, "no focused client"
	})
	id, err := s.Client(t).ActiveWindow()
	if err != nil || id != "" {
		t.Fatalf("ActiveWindow() = %q, %v", id, err)
	}
}

func TestCursorPositionRoundsFractionalMangoCoordinates(t *testing.T) {
	s := newFakeServer(t, func(_ *testing.T, parts []string) (string, bool, bool, string) {
		if strings.Join(parts, " ") == "get cursorpos" {
			return `{"x":100.6,"y":200.2}`, false, true, ""
		}
		return "", true, false, "unexpected request"
	})
	x, y, err := s.Client(t).GetCursorPosition()
	if err != nil || x != 101 || y != 200 {
		t.Fatalf("GetCursorPosition() = %d, %d, %v", x, y, err)
	}
}

func TestWindowActionsUseTargetClientAndNativeDispatchSyntax(t *testing.T) {
	s := newFakeServer(t, func(_ *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 3 && parts[0] == "get" && parts[1] == "client" {
			return `{"id":9,"monitor":"DP-1","is_floating":false,"width":800,"height":600}`, false, true, ""
		}
		return "", true, true, ""
	})
	m := s.Client(t)
	checks := []struct {
		call func() error
		want string
	}{
		{func() error { return m.MoveWindow("9", "l") }, "dispatch move_client,left client,9"},
		{func() error { return m.ResizeWindow("9", 1000, 700) }, "dispatch resizewin,200,100 client,9"},
		{func() error { return m.ToggleFloating("9") }, "dispatch togglefloating client,9"},
		{func() error { return m.MoveWindowPixel("9", 100, 200) }, "dispatch movewin,100,200 client,9"},
	}
	for _, check := range checks {
		if err := check.call(); err != nil {
			t.Fatal(err)
		}
		requests := s.Requests()
		if requests[len(requests)-1] != check.want {
			t.Fatalf("request = %q, want %q", requests[len(requests)-1], check.want)
		}
	}
}

func TestMonitorAndSystemActionsUseNativeDispatchSyntax(t *testing.T) {
	s := newFakeServer(t, func(_ *testing.T, _ []string) (string, bool, bool, string) {
		return "", true, true, ""
	})
	m := s.Client(t)
	for _, call := range []func() error{
		func() error { return m.FocusDir("l") },
		func() error { return m.FocusMonitor("DP-2") },
		func() error { return m.MoveToMonitor("7", "DP-2") },
		func() error { return m.SetDpms("DP-2", false) },
		func() error { return m.ToggleOverview() },
		func() error { return m.ReloadConfig() },
		func() error { return m.Exit() },
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
	}
	requests := s.Requests()
	for _, want := range []string{
		"dispatch focusdir,left",
		"dispatch focusmon,DP-2",
		"dispatch tagmon,DP-2 client,7",
		"dispatch sleep_monitor,DP-2",
		"dispatch toggleoverview",
		"dispatch reload_config",
		"dispatch quit",
	} {
		if !containsString(requests, want) {
			t.Errorf("missing %q from %v", want, requests)
		}
	}
}

func TestStateSettersAvoidUnnecessaryToggles(t *testing.T) {
	dispatches := 0
	s := newFakeServer(t, func(_ *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 3 && parts[0] == "get" && parts[1] == "client" {
			return `{"id":4,"is_fullscreen":true,"is_maximized":true,"is_global":true,"is_fakefullscreen":true}`, false, true, ""
		}
		if len(parts) > 0 && parts[0] == "dispatch" {
			dispatches++
		}
		return "", true, true, ""
	})
	m := s.Client(t)
	for _, call := range []func() error{
		func() error { return m.SetFullscreen("4", true) },
		func() error { return m.SetMaximized("4", true) },
		func() error { return m.PinWindow("4", true) },
		func() error { return m.SetFakeFullscreen("4", true) },
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
	}
	if dispatches != 0 {
		t.Fatalf("dispatches = %d, want 0", dispatches)
	}
}

func TestStateSettersDispatchWhenStateDiffers(t *testing.T) {
	s := newFakeServer(t, func(_ *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 3 && parts[0] == "get" && parts[1] == "client" {
			return `{"id":4,"is_fullscreen":false,"is_maximized":false,"is_global":false,"is_fakefullscreen":false}`, false, true, ""
		}
		return "", true, true, ""
	})
	m := s.Client(t)
	for _, call := range []func() error{
		func() error { return m.SetFullscreen("4", true) },
		func() error { return m.SetMaximized("4", true) },
		func() error { return m.PinWindow("4", true) },
		func() error { return m.SetFakeFullscreen("4", true) },
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
	}
	wants := map[string]bool{
		"dispatch togglefullscreen client,4":     false,
		"dispatch togglemaximizescreen client,4": false,
		"dispatch toggleglobal client,4":         false,
		"dispatch togglefakefullscreen client,4": false,
	}
	for _, request := range s.Requests() {
		if _, ok := wants[request]; ok {
			wants[request] = true
		}
	}
	for request, found := range wants {
		if !found {
			t.Errorf("missing %q in requests %v", request, s.Requests())
		}
	}
}

func TestWorkspaceIDsAndSpecialTag(t *testing.T) {
	s := newFakeServer(t, func(_ *testing.T, parts []string) (string, bool, bool, string) {
		switch strings.Join(parts, " ") {
		case "get all-tags":
			return `{"all_tags":[{"monitor":"DP-1","tags":[{"index":1,"is_active":true,"is_urgent":false,"layout":"T","client_count":2},{"index":2,"is_active":false,"is_urgent":true,"layout":"G","client_count":0}]}]}`, false, true, ""
		case "get layouts":
			return `{"layouts":[{"symbol":"T","name":"tile"},{"symbol":"G","name":"grid"}]}`, false, true, ""
		case "get all-monitors":
			return `{"monitors":[{"name":"DP-1","active":true,"x":0,"y":0,"width":1920,"height":1080,"scale":1.25,"active_tags":[1],"layout_index":0}]}`, false, true, ""
		default:
			return "", true, true, ""
		}
	})
	m := s.Client(t)
	workspaces, err := m.ListWorkspaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 3 || workspaces[0].ID != "DP-1:1" || !workspaces[0].IsActive || workspaces[1].ID != "DP-1:2" || !workspaces[1].IsEmpty || workspaces[2].ID != "DP-1:0" {
		t.Fatalf("workspaces = %+v", workspaces)
	}
	if workspaces[0].Metadata["layout"] != "tile" || workspaces[0].Metadata["layout_symbol"] != "T" || workspaces[1].Metadata["layout"] != "grid" {
		t.Fatalf("workspace layouts = %+v / %+v", workspaces[0].Metadata, workspaces[1].Metadata)
	}
	active, err := m.ActiveWorkspace()
	if err != nil || active.ID != "DP-1:1" {
		t.Fatalf("ActiveWorkspace() = %+v, %v", active, err)
	}
	if err := m.SwitchWorkspace("r+1"); err != nil {
		t.Fatal(err)
	}
	if err := m.SwitchWorkspace("DP-1:3"); err != nil {
		t.Fatal(err)
	}
	if err := m.MoveToWorkspace("9", "DP-1:3"); err != nil {
		t.Fatal(err)
	}
	if err := m.MoveToWorkspaceSilent("9", "DP-1:2"); err != nil {
		t.Fatal(err)
	}
	requests := s.Requests()
	wants := []string{"dispatch viewtoright", "dispatch view,3", "dispatch tagcrossmon,3,DP-1 client,9", "dispatch tagsilent,2 client,9"}
	for _, want := range wants {
		if !containsString(requests, want) {
			t.Errorf("missing %q from %v", want, requests)
		}
	}
	if err := m.ToggleSpecialWorkspace(""); err != nil {
		t.Fatal(err)
	}
	if requests := s.Requests(); requests[len(requests)-1] != "dispatch toggle_special_tag" {
		t.Fatalf("special tag request = %q", requests[len(requests)-1])
	}
	if err := m.ToggleSpecialWorkspace("named"); err != ipc.ErrNotSupported {
		t.Fatalf("named special workspace error = %v", err)
	}
}

func TestOverviewTagIsReportedSeparatelyFromWorkspaceState(t *testing.T) {
	s := newFakeServer(t, func(_ *testing.T, parts []string) (string, bool, bool, string) {
		switch strings.Join(parts, " ") {
		case "get all-tags":
			return `{"all_tags":[{"monitor":"DP-1","tags":[{"index":1,"is_active":true,"is_urgent":false,"layout":"T","client_count":1},{"index":2,"is_active":false,"is_urgent":false,"layout":"G","client_count":0}]}]}`, false, true, ""
		case "get layouts":
			return `{"layouts":[{"symbol":"T","name":"tile"},{"symbol":"G","name":"grid"}]}`, false, true, ""
		case "get all-monitors":
			return `{"monitors":[{"name":"DP-1","active":true,"active_tags":[0]}]}`, false, true, ""
		case "get all-clients":
			return `{"clients":[]}`, false, true, ""
		default:
			return "", true, true, ""
		}
	})
	m := s.Client(t)

	monitors, err := m.ListMonitors()
	if err != nil {
		t.Fatal(err)
	}
	if got := monitors[0].Metadata["overview_open"]; got != true {
		t.Fatalf("overview_open = %v, want true", got)
	}

	workspaces, err := m.ListWorkspaces()
	if err != nil {
		t.Fatal(err)
	}
	if !workspaces[0].IsActive || workspaces[1].IsActive || workspaces[2].IsActive {
		t.Fatalf("overview changed the underlying workspace state: %+v", workspaces)
	}
}

func TestSilentCrossMonitorMoveIsUnsupported(t *testing.T) {
	s := newFakeServer(t, func(_ *testing.T, parts []string) (string, bool, bool, string) {
		if strings.Join(parts, " ") == "get all-monitors" {
			return `{"monitors":[{"name":"DP-1","active":true}]}`, false, true, ""
		}
		return "", true, true, ""
	})
	if err := s.Client(t).MoveToWorkspaceSilent("5", "HDMI-A-1:2"); err != ipc.ErrNotSupported {
		t.Fatalf("MoveToWorkspaceSilent error = %v", err)
	}
}

func TestMonitorAndLayoutMapping(t *testing.T) {
	s := newFakeServer(t, func(_ *testing.T, parts []string) (string, bool, bool, string) {
		switch strings.Join(parts, " ") {
		case "get all-monitors":
			return `{"monitors":[{"name":"DP-1","active":true,"x":0,"y":0,"width":1440,"height":900,"scale":1.5,"layout_index":1,"layout_symbol":"[]="}]}`, false, true, ""
		case "get layouts":
			return `{"layouts":[{"symbol":"[]","name":"tile"},{"symbol":"[]=","name":"scroller"}]}`, false, true, ""
		default:
			return "", true, true, ""
		}
	})
	m := s.Client(t)
	monitors, err := m.ListMonitors()
	if err != nil || len(monitors) != 1 || monitors[0].Scale != 1.5 || monitors[0].LogicalWidth != 1440 || !monitors[0].IsFocused {
		t.Fatalf("ListMonitors() = %+v, %v", monitors, err)
	}
	layouts, err := m.ListLayouts()
	if err != nil || len(layouts) != 2 || !layouts[1].Current || layouts[1].Name != "scroller" {
		t.Fatalf("ListLayouts() = %+v, %v", layouts, err)
	}
	if err := m.SetLayout("scroller"); err != nil {
		t.Fatal(err)
	}
	if got := s.Requests()[len(s.Requests())-1]; got != "dispatch setlayout,scroller" {
		t.Fatalf("SetLayout request = %q", got)
	}
}

func TestDynamicConfigAndSystemDispatches(t *testing.T) {
	s := newFakeServer(t, func(_ *testing.T, parts []string) (string, bool, bool, string) {
		if len(parts) >= 3 && parts[0] == "get" && parts[1] == "client" {
			return `{"id":8}`, false, true, ""
		}
		return "", true, true, ""
	})
	m := s.Client(t)
	for _, call := range []func() error{
		func() error { return m.SetConfig("border.width", 2) },
		func() error { return m.SetConfig("gaps.inner", 5) },
		func() error { return m.SetKeyboardLayouts("us,es", "altgr-intl,") },
		func() error { return m.SwitchKeyboardLayout("next") },
		func() error { return m.Execute("foot --app-id test") },
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
	}
	requests := s.Requests()
	for _, want := range []string{
		"dispatch setoption,border_px,2",
		"dispatch setoption,gap_inner_horizontal,5",
		"dispatch setoption,gap_inner_vertical,5",
		"dispatch setoption,xkb_rules_layout,us,es",
		"dispatch setoption,xkb_rules_variant,altgr-intl,",
		"dispatch switch_keyboard_layout,0",
		"dispatch spawn_shell,foot --app-id test",
	} {
		if !containsString(requests, want) {
			t.Errorf("missing %q from %v", want, requests)
		}
	}
	if err := m.SwitchKeyboardLayout("prev"); err != ipc.ErrNotSupported {
		t.Fatalf("previous keyboard layout error = %v", err)
	}
}

func TestLayoutPropertyAndGroupNavigation(t *testing.T) {
	s := newFakeServer(t, func(_ *testing.T, parts []string) (string, bool, bool, string) {
		switch strings.Join(parts, " ") {
		case "get client 8":
			return `{"id":8,"monitor":"DP-1","scroller_proportion":0.7}`, false, true, ""
		case "get layouts":
			return `{"layouts":[{"name":"tile"},{"name":"scroller"}]}`, false, true, ""
		case "get all-monitors":
			return `{"monitors":[{"name":"DP-1","active":true,"layout_index":1}]}`, false, true, ""
		default:
			return "", true, true, ""
		}
	})
	m := s.Client(t)
	if err := m.SetLayoutProperty("8", "scroller_proportion", "0.75"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetLayoutProperty("8", "scroller_proportion", "+0.1"); err != nil {
		t.Fatal(err)
	}
	if err := m.GroupNav("b"); err != nil {
		t.Fatal(err)
	}
	requests := s.Requests()
	if !containsString(requests, "dispatch set_proportion,0.75 client,8") ||
		!containsString(requests, "dispatch set_proportion,0.8 client,8") ||
		!containsString(requests, "dispatch groupfocus,prev") {
		t.Fatalf("requests = %v", requests)
	}
}

func TestCapabilities(t *testing.T) {
	caps, err := (&Mango{}).GetCapabilities()
	if err != nil || !caps.Blur || !caps.Shadows || !caps.Animations || !caps.RoundedCorners || !caps.WindowsSupported || !caps.WorkspacesSupported {
		t.Fatalf("GetCapabilities() = %+v, %v", caps, err)
	}
}

func TestUnsupportedOperationsAreExplicit(t *testing.T) {
	m := &Mango{}
	if err := m.ToggleGroup("1"); err != ipc.ErrNotSupported {
		t.Fatalf("ToggleGroup() = %v", err)
	}
	if err := m.SetLayoutProperty("", "unknown", "1"); err != ipc.ErrNotSupported {
		t.Fatalf("SetLayoutProperty() = %v", err)
	}
	if _, err := m.GetAnimations(); err != ipc.ErrNotSupported {
		t.Fatalf("GetAnimations() = %v", err)
	}
	if err := m.BatchKeybinds("{}"); err != ipc.ErrNotSupported {
		t.Fatalf("BatchKeybinds() = %v", err)
	}
	if err := m.RawBatch("{}"); err != ipc.ErrNotSupported {
		t.Fatalf("RawBatch() = %v", err)
	}
}

func TestParseClientAndWorkspaceIDs(t *testing.T) {
	if _, err := parseClientID("abc"); err == nil {
		t.Fatal("expected invalid client ID error")
	}
	if _, err := clientTarget(""); err == nil {
		t.Fatal("expected missing client ID error")
	}
	if tag, err := parseTag("0"); err != nil || tag != 0 {
		t.Fatalf("parseTag(0) = %d, %v", tag, err)
	}
	if monitor, tag, err := (&Mango{}).resolveWorkspace("DP-1:3"); err != nil || monitor != "DP-1" || tag != 3 {
		t.Fatalf("resolveWorkspace() = %q, %d, %v", monitor, tag, err)
	}
}

func TestMapWatchEnvelopesAndDiffSnapshots(t *testing.T) {
	watch := &watchSession{}
	clients := watch.mapLine([]byte(`{"clients":[{"id":1,"title":"a","appid":"foot","monitor":"DP-1","tags":[1],"is_visible":true,"is_focused":true}]}`))
	if clients.Type != mangoClientsSnapshot {
		t.Fatalf("client snapshot type = %q", clients.Type)
	}
	windows := clients.Payload["windows"].([]ipc.Window)
	previous, events := snapshotEvents(nil, windows, 1)
	if len(events) != 1 || events[0].Type != ipc.EventWorkspaceChanged {
		t.Fatalf("initial snapshot events = %+v", events)
	}
	updated := append([]ipc.Window(nil), windows...)
	updated = append(updated, ipc.Window{ID: "2", Title: "b"})
	next, events := snapshotEvents(previous, updated, 2)
	if len(events) != 1 || events[0].Type != ipc.EventWindowCreated || events[0].Window.ID != "2" {
		t.Fatalf("new window events = %+v", events)
	}
	_, events = snapshotEvents(next, updated[:1], 3)
	if len(events) != 1 || events[0].Type != ipc.EventWindowClosed || events[0].Payload["id"] != "2" {
		t.Fatalf("closed window events = %+v", events)
	}
	monitors := watch.mapLine([]byte(`{"monitors":[{"name":"DP-1","active":true}]}`))
	if monitors.Type != ipc.EventMonitorChanged {
		t.Fatalf("monitor event type = %q", monitors.Type)
	}
	tags := watch.mapLine([]byte(`{"all_tags":[{"monitor":"DP-1","tags":[{"index":1,"is_active":true,"client_count":1}]}]}`))
	if tags.Type != ipc.EventWorkspaceChanged {
		t.Fatalf("workspace event type = %q", tags.Type)
	}
}

func TestSubscribeUsesSeparateNativeWatchConnections(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "mango.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	var mu sync.Mutex
	var requests []string
	go func() {
		for i := 0; i < 4; i++ {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				line, err := bufio.NewReader(c).ReadString('\n')
				if err != nil {
					return
				}
				request := strings.TrimSpace(line)
				mu.Lock()
				requests = append(requests, request)
				mu.Unlock()
				var initial string
				switch request {
				case "watch all-clients":
					initial = `{"clients":[]}`
				case "watch all-monitors":
					initial = `{"monitors":[]}`
				case "watch all-tags":
					initial = `{"all_tags":[]}`
				case "watch focusing-client":
					initial = `{"id":null,"title":null,"appid":null}`
				}
				_, _ = fmt.Fprintln(c, initial)
				buffer := make([]byte, 1)
				_, _ = c.Read(buffer)
			}(conn)
		}
	}()
	m := &Mango{socketPath: socket}
	events, err := m.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Type == "" {
			t.Fatal("empty initial event")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial watch state")
	}
	deadline := time.After(2 * time.Second)
	var got []string
	for len(got) < 4 {
		mu.Lock()
		got = append(got[:0], requests...)
		mu.Unlock()
		if len(got) == 4 {
			break
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatalf("timed out waiting for four watch requests; received %v", got)
		}
	}
	if len(got) != 4 {
		t.Fatalf("watch connections = %v", got)
	}
	for _, want := range []string{"watch all-clients", "watch all-monitors", "watch all-tags", "watch focusing-client"} {
		if !containsString(got, want) {
			t.Errorf("missing %q in watch requests %v", want, got)
		}
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSocketPathAbsolute(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "mango.sock")
	t.Setenv("MANGO_INSTANCE_SIGNATURE", socket)
	got, err := resolveSocketPath()
	if err != nil || got != socket {
		t.Fatalf("resolveSocketPath() = %q, %v", got, err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
