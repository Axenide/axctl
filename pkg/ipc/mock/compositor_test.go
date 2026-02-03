package mock

import (
	"testing"
	"time"

	"axctl/pkg/ipc"
)

func TestNewCompositor(t *testing.T) {
	m := NewCompositor()

	if m == nil {
		t.Fatal("NewCompositor returned nil")
	}

	windows, err := m.ListWindows()
	if err != nil {
		t.Fatalf("ListWindows failed: %v", err)
	}
	if len(windows) != 0 {
		t.Fatalf("expected 0 windows, got %d", len(windows))
	}

	workspaces, err := m.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces failed: %v", err)
	}
	if len(workspaces) != 0 {
		t.Fatalf("expected 0 workspaces, got %d", len(workspaces))
	}
}

func TestAddWindow(t *testing.T) {
	m := NewCompositor()

	w1 := ipc.Window{
		ID:          "win1",
		Title:       "Test Window",
		Class:       "test",
		WorkspaceID: "ws1",
	}

	m.AddWindow(w1)

	windows, _ := m.ListWindows()
	if len(windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(windows))
	}
	if windows[0].ID != "win1" {
		t.Fatalf("expected window ID 'win1', got '%s'", windows[0].ID)
	}
}

func TestAddWorkspace(t *testing.T) {
	m := NewCompositor()

	ws1 := ipc.Workspace{
		ID:        "ws1",
		Name:      "Work",
		MonitorID: "mon1",
	}

	m.AddWorkspace(ws1)

	workspaces, _ := m.ListWorkspaces()
	if len(workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(workspaces))
	}
	if workspaces[0].ID != "ws1" {
		t.Fatalf("expected workspace ID 'ws1', got '%s'", workspaces[0].ID)
	}
}

func TestFocusWindowSuccess(t *testing.T) {
	m := NewCompositor()

	w := ipc.Window{ID: "win1", Title: "Test", Class: "test", WorkspaceID: "ws1"}
	m.AddWindow(w)

	err := m.FocusWindow("win1")
	if err != nil {
		t.Fatalf("FocusWindow failed: %v", err)
	}

	calls := m.FocusWindowCalls()
	if len(calls) != 1 || calls[0] != "win1" {
		t.Fatalf("expected FocusWindow called with ['win1'], got %v", calls)
	}
}

func TestFocusWindowNotFound(t *testing.T) {
	m := NewCompositor()

	err := m.FocusWindow("nonexistent")
	if err != ipc.ErrWindowNotFound {
		t.Fatalf("expected ErrWindowNotFound, got %v", err)
	}
}

func TestFocusWindowError(t *testing.T) {
	m := NewCompositor()

	customErr := ipc.NewError("TEST_ERROR", "test error", nil)
	m.SetFocusWindowError(customErr)

	err := m.FocusWindow("win1")
	if err != customErr {
		t.Fatalf("expected custom error, got %v", err)
	}
}

func TestCloseWindowSuccess(t *testing.T) {
	m := NewCompositor()

	w := ipc.Window{ID: "win1", Title: "Test", Class: "test", WorkspaceID: "ws1"}
	m.AddWindow(w)

	err := m.CloseWindow("win1")
	if err != nil {
		t.Fatalf("CloseWindow failed: %v", err)
	}

	windows, _ := m.ListWindows()
	if len(windows) != 0 {
		t.Fatalf("expected 0 windows after close, got %d", len(windows))
	}

	calls := m.CloseWindowCalls()
	if len(calls) != 1 || calls[0] != "win1" {
		t.Fatalf("expected CloseWindow called with ['win1'], got %v", calls)
	}
}

func TestCloseWindowNotFound(t *testing.T) {
	m := NewCompositor()

	err := m.CloseWindow("nonexistent")
	if err != ipc.ErrWindowNotFound {
		t.Fatalf("expected ErrWindowNotFound, got %v", err)
	}
}

func TestSwitchWorkspaceSuccess(t *testing.T) {
	m := NewCompositor()

	ws := ipc.Workspace{ID: "ws1", Name: "Work", MonitorID: "mon1"}
	m.AddWorkspace(ws)

	err := m.SwitchWorkspace("ws1")
	if err != nil {
		t.Fatalf("SwitchWorkspace failed: %v", err)
	}

	calls := m.SwitchWorkspaceCalls()
	if len(calls) != 1 || calls[0] != "ws1" {
		t.Fatalf("expected SwitchWorkspace called with ['ws1'], got %v", calls)
	}
}

func TestSwitchWorkspaceNotFound(t *testing.T) {
	m := NewCompositor()

	err := m.SwitchWorkspace("nonexistent")
	if err != ipc.ErrWorkspaceNotFound {
		t.Fatalf("expected ErrWorkspaceNotFound, got %v", err)
	}
}

func TestSubscribeSuccess(t *testing.T) {
	m := NewCompositor()

	ch, err := m.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	if ch == nil {
		t.Fatal("Subscribe returned nil channel")
	}

	calls := m.SubscribeCalls()
	if calls != 1 {
		t.Fatalf("expected Subscribe called 1 time, got %d", calls)
	}

	m.Close()
}

func TestSubscribeError(t *testing.T) {
	m := NewCompositor()

	customErr := ipc.NewError("SUBSCRIBE_ERROR", "cannot subscribe", nil)
	m.SetSubscribeError(customErr)

	ch, err := m.Subscribe()
	if err != customErr {
		t.Fatalf("expected custom error, got %v", err)
	}

	if ch != nil {
		t.Fatal("expected nil channel on error")
	}
}

func TestEmitEvent(t *testing.T) {
	m := NewCompositor()

	ch, _ := m.Subscribe()

	evt := ipc.Event{
		Type:      ipc.EventWindowCreated,
		Timestamp: time.Now().Unix(),
		Window: &ipc.Window{
			ID:          "win1",
			Title:       "New Window",
			Class:       "app",
			WorkspaceID: "ws1",
		},
	}

	m.EmitEvent(evt)

	// Receive event with timeout
	select {
	case received := <-ch:
		if received.Window.ID != "win1" {
			t.Fatalf("expected window ID 'win1', got '%s'", received.Window.ID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}

	m.Close()
}

func TestCallTracking(t *testing.T) {
	m := NewCompositor()

	w1 := ipc.Window{ID: "win1", Title: "Test1", Class: "test", WorkspaceID: "ws1"}
	w2 := ipc.Window{ID: "win2", Title: "Test2", Class: "test", WorkspaceID: "ws1"}
	m.AddWindow(w1)
	m.AddWindow(w2)

	m.ListWindows()
	m.ListWindows()
	m.FocusWindow("win1")
	m.FocusWindow("win2")
	m.CloseWindow("win1")

	if m.ListWindowsCalls() != 2 {
		t.Fatalf("expected ListWindows called 2 times, got %d", m.ListWindowsCalls())
	}

	focusCalls := m.FocusWindowCalls()
	if len(focusCalls) != 2 || focusCalls[0] != "win1" || focusCalls[1] != "win2" {
		t.Fatalf("expected FocusWindow called with ['win1', 'win2'], got %v", focusCalls)
	}

	closeCalls := m.CloseWindowCalls()
	if len(closeCalls) != 1 || closeCalls[0] != "win1" {
		t.Fatalf("expected CloseWindow called with ['win1'], got %v", closeCalls)
	}
}

func TestReset(t *testing.T) {
	m := NewCompositor()

	w := ipc.Window{ID: "win1", Title: "Test", Class: "test", WorkspaceID: "ws1"}
	m.AddWindow(w)
	m.ListWindows()
	m.FocusWindow("win1")

	m.Reset()

	if m.ListWindowsCalls() != 0 {
		t.Fatalf("expected ListWindowsCalls 0 after reset, got %d", m.ListWindowsCalls())
	}

	windows, _ := m.ListWindows()
	if len(windows) != 0 {
		t.Fatalf("expected 0 windows after reset, got %d", len(windows))
	}

	focusCalls := m.FocusWindowCalls()
	if len(focusCalls) != 0 {
		t.Fatalf("expected FocusWindowCalls empty after reset, got %v", focusCalls)
	}
}

func TestWindowToJSON(t *testing.T) {
	w := ipc.Window{
		ID:          "win1",
		Title:       "Test Window",
		Class:       "test",
		WorkspaceID: "ws1",
	}

	data := WindowToJSON(w)
	if len(data) == 0 {
		t.Fatal("WindowToJSON returned empty bytes")
	}

	w2, err := WindowFromJSON(data)
	if err != nil {
		t.Fatalf("WindowFromJSON failed: %v", err)
	}

	if w2.ID != w.ID || w2.Title != w.Title {
		t.Fatalf("JSON roundtrip failed: %v -> %v", w, w2)
	}
}

func TestEventToText(t *testing.T) {
	tests := []struct {
		name     string
		evt      ipc.Event
		expected string
	}{
		{
			name: "window created",
			evt: ipc.Event{
				Type:   ipc.EventWindowCreated,
				Window: &ipc.Window{ID: "win1", Title: "New App"},
			},
			expected: "WINDOW_CREATED: win1 (New App)",
		},
		{
			name: "window focused",
			evt: ipc.Event{
				Type:   ipc.EventWindowFocused,
				Window: &ipc.Window{ID: "win2", Title: "Active"},
			},
			expected: "WINDOW_FOCUSED: win2 (Active)",
		},
		{
			name: "workspace changed",
			evt: ipc.Event{
				Type:      ipc.EventWorkspaceChanged,
				Workspace: &ipc.Workspace{ID: "ws1", Name: "Work"},
			},
			expected: "WORKSPACE_CHANGED: ws1 (Work)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := EventToText(tc.evt)
			if result != tc.expected {
				t.Fatalf("expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}
