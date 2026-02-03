package ipc

// Compositor defines the interface for a universal Wayland compositor IPC daemon.
// Implementations should support Hyprland, Niri, and Mangowc compositors.
type Compositor interface {
	// ListWindows returns all currently open windows.
	// Returns an error if the compositor cannot be queried.
	ListWindows() ([]Window, error)

	// FocusWindow brings the window with the given ID into focus.
	// Returns ErrWindowNotFound if the window does not exist.
	FocusWindow(id string) error

	// CloseWindow closes the window with the given ID.
	// Returns ErrWindowNotFound if the window does not exist.
	CloseWindow(id string) error

	// ListWorkspaces returns all workspaces known to the compositor.
	// Returns an error if the compositor cannot be queried.
	ListWorkspaces() ([]Workspace, error)

	// SwitchWorkspace switches to the workspace with the given ID.
	// Returns ErrWorkspaceNotFound if the workspace does not exist.
	SwitchWorkspace(id string) error

	// Subscribe returns a channel that emits compositor events.
	// The channel should be closed by the implementation when monitoring stops.
	// Returns an error if the subscription cannot be established.
	Subscribe() (<-chan Event, error)
}
