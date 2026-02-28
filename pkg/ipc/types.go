package ipc

type Window struct {
	ID           string                 `json:"id"`
	Title        string                 `json:"title"`
	AppID        string                 `json:"app_id"`
	WorkspaceID  string                 `json:"workspace_id"`
	IsFocused    bool                   `json:"is_focused"`
	IsFloating   bool                   `json:"is_floating"`
	IsFullscreen bool                   `json:"is_fullscreen"`
	IsHidden     bool                   `json:"is_hidden"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

type Workspace struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	MonitorID string                 `json:"monitor_id"`
	IsActive  bool                   `json:"is_active"`
	IsEmpty   bool                   `json:"is_empty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type Monitor struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Width       int                    `json:"width"`
	Height      int                    `json:"height"`
	RefreshRate float64                `json:"refresh_rate,omitempty"`
	Scale       float64                `json:"scale,omitempty"`
	IsFocused   bool                   `json:"is_focused"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type Capabilities struct {
	Blur                bool `json:"blur"`
	Shadows             bool `json:"shadows"`
	Animations          bool `json:"animations"`
	RoundedCorners      bool `json:"rounded_corners"`
	WorkspacesSupported bool `json:"workspaces_supported"`
	WindowsSupported    bool `json:"windows_supported"`
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
	// EventWindowMoved is fired when a window is moved to another workspace.
	EventWindowMoved EventType = "window_moved"
	// EventWorkspaceChanged is fired when a workspace changes.
	EventWorkspaceChanged EventType = "workspace_changed"
	// EventMonitorChanged is fired when monitor layout changes.
	EventMonitorChanged EventType = "monitor_changed"
	// EventConfigReloaded is fired when the compositor config is reloaded.
	EventConfigReloaded EventType = "config_reloaded"
	// EventFullscreenChanged is fired when a window's fullscreen state changes.
	EventFullscreenChanged EventType = "fullscreen_changed"
	// EventFocusedMonitorChanged is fired when the focused monitor changes.
	EventFocusedMonitorChanged EventType = "focused_monitor_changed"
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
