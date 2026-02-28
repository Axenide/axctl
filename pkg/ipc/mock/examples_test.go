package mock

import (
	"testing"
	"time"

	"axctl/pkg/ipc"
)

func TestExampleAdapter(t *testing.T) {
	m := NewCompositor()
	defer m.Close()

	// Setup initial state
	m.AddWindow(ipc.Window{
		ID:          "0x1a2b",
		Title:       "Firefox",
		AppID:       "firefox",
		WorkspaceID: "1",
	})
	m.AddWindow(ipc.Window{
		ID:          "0x3c4d",
		Title:       "Terminal",
		AppID:       "alacritty",
		WorkspaceID: "1",
	})
	m.AddWorkspace(ipc.Workspace{
		ID:        "1",
		Name:      "General",
		MonitorID: "eDP-1",
	})

	// Example: Adapter that lists and filters windows
	allWindows, _ := m.ListWindows()
	if len(allWindows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(allWindows))
	}

	// Example: Adapter that switches workspaces
	_ = m.SwitchWorkspace("1")
	switchCalls := m.SwitchWorkspaceCalls()
	if len(switchCalls) != 1 {
		t.Fatalf("expected 1 switch call, got %d", len(switchCalls))
	}
}

// ExampleErrorHandling demonstrates testing error scenarios.
func TestExampleErrorHandling(t *testing.T) {
	m := NewCompositor()
	defer m.Close()

	// Setup error responses
	m.SetListWindowsError(ipc.ErrCompositorNotAvailable)

	// Verify adapter handles errors
	_, err := m.ListWindows()
	if err != ipc.ErrCompositorNotAvailable {
		t.Fatalf("expected ErrCompositorNotAvailable")
	}

	// Reset and test different error
	m.SetListWindowsError(nil)
	m.SetFocusWindowError(ipc.NewError("CUSTOM", "cannot focus", nil))

	err = m.FocusWindow("nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
}

// ExampleEventStreaming demonstrates using the event subscription channel
// to test adapters that react to compositor events.
func TestExampleEventStreaming(t *testing.T) {
	m := NewCompositor()
	defer m.Close()

	ch, err := m.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Emit window created event
	m.EmitEvent(ipc.Event{
		Type:      ipc.EventWindowCreated,
		Timestamp: time.Now().Unix(),
		Window: &ipc.Window{
			ID:          "new-win",
			Title:       "New App",
			AppID:       "app",
			WorkspaceID: "1",
		},
	})

	// Adapter receives and processes event
	evt := <-ch
	if evt.Type != ipc.EventWindowCreated {
		t.Fatalf("expected EventWindowCreated")
	}

	// Emit workspace changed event
	m.EmitEvent(ipc.Event{
		Type:      ipc.EventWorkspaceChanged,
		Timestamp: time.Now().Unix(),
		Workspace: &ipc.Workspace{
			ID:        "2",
			Name:      "Work",
			MonitorID: "eDP-1",
		},
	})

	evt = <-ch
	if evt.Type != ipc.EventWorkspaceChanged {
		t.Fatalf("expected EventWorkspaceChanged")
	}
}

// ExampleCallTracking demonstrates verifying that an adapter
// calls the Compositor methods correctly.
func TestExampleCallTracking(t *testing.T) {
	m := NewCompositor()
	defer m.Close()

	w1 := ipc.Window{ID: "win1", Title: "App1", AppID: "app1", WorkspaceID: "1"}
	w2 := ipc.Window{ID: "win2", Title: "App2", AppID: "app2", WorkspaceID: "2"}
	m.AddWindow(w1)
	m.AddWindow(w2)

	// Simulate adapter operations
	m.ListWindows()
	m.ListWindows()
	m.FocusWindow("win1")
	m.FocusWindow("win2")
	m.CloseWindow("win1")

	// Verify correct calls
	if m.ListWindowsCalls() != 2 {
		t.Fatalf("ListWindows should be called 2 times")
	}

	focusCalls := m.FocusWindowCalls()
	if len(focusCalls) != 2 {
		t.Fatalf("FocusWindow should be called 2 times")
	}

	closeCalls := m.CloseWindowCalls()
	if len(closeCalls) != 1 || closeCalls[0] != "win1" {
		t.Fatalf("CloseWindow should be called with 'win1'")
	}
}

// ExampleJSONSerialization demonstrates converting compositor types
// to/from JSON, useful for testing serialization/deserialization adapters.
func TestExampleJSONSerialization(t *testing.T) {
	original := ipc.Window{
		ID:          "0x123",
		Title:       "Firefox",
		AppID:       "firefox",
		WorkspaceID: "1",
	}

	// Serialize to JSON
	data := WindowToJSON(original)

	// Deserialize from JSON
	restored, err := WindowFromJSON(data)
	if err != nil {
		t.Fatalf("WindowFromJSON failed: %v", err)
	}

	// Verify roundtrip
	if restored.ID != original.ID || restored.Title != original.Title {
		t.Fatalf("JSON roundtrip failed")
	}

	// Same for events
	evt := ipc.Event{
		Type:      ipc.EventWindowFocused,
		Timestamp: time.Now().Unix(),
		Window:    &original,
	}

	eventData := EventToJSON(evt)
	restoredEvt, err := EventFromJSON(eventData)
	if err != nil {
		t.Fatalf("EventFromJSON failed: %v", err)
	}

	if restoredEvt.Type != evt.Type {
		t.Fatalf("Event roundtrip failed")
	}
}

// ExampleTextFormatting demonstrates the EventToText helper
// for human-readable event representations.
func TestExampleTextFormatting(t *testing.T) {
	evt := ipc.Event{
		Type: ipc.EventWindowFocused,
		Window: &ipc.Window{
			ID:    "0x123",
			Title: "Code Editor",
		},
	}

	text := EventToText(evt)
	expected := "WINDOW_FOCUSED: 0x123 (Code Editor)"
	if text != expected {
		t.Fatalf("expected '%s', got '%s'", expected, text)
	}
}
