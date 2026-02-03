package ipc

// Window represents a window in the compositor.
type Window struct {
	// ID is the unique identifier for the window.
	ID string
	// Title is the window's title string.
	Title string
	// Class is the application class name.
	Class string
	// WorkspaceID is the ID of the workspace containing this window.
	WorkspaceID string
}

// Workspace represents a workspace in the compositor.
type Workspace struct {
	// ID is the unique identifier for the workspace.
	ID string
	// Name is the human-readable name of the workspace.
	Name string
	// MonitorID is the ID of the monitor displaying this workspace.
	MonitorID string
}

// EventType represents the type of event occurring in the compositor.
type EventType string

const (
	// EventWindowCreated is fired when a new window is created.
	EventWindowCreated EventType = "window_created"
	// EventWindowClosed is fired when a window is closed.
	EventWindowClosed EventType = "window_closed"
	// EventWindowFocused is fired when a window gains focus.
	EventWindowFocused EventType = "window_focused"
	// EventWindowTitleChanged is fired when a window's title changes.
	EventWindowTitleChanged EventType = "window_title_changed"
	// EventWorkspaceChanged is fired when a workspace changes.
	EventWorkspaceChanged EventType = "workspace_changed"
	// EventMonitorChanged is fired when monitor layout changes.
	EventMonitorChanged EventType = "monitor_changed"
)

// Event represents a compositor event.
type Event struct {
	// Type is the type of event.
	Type EventType
	// Timestamp is the Unix timestamp when the event occurred.
	Timestamp int64
	// Window is the affected window (nil if not applicable).
	Window *Window
	// Workspace is the affected workspace (nil if not applicable).
	Workspace *Workspace
	// Payload contains additional event-specific data.
	Payload map[string]interface{}
}
