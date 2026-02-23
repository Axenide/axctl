package ipc

import "fmt"

type Compositor interface {
	ListWindows() ([]Window, error)
	ActiveWindow() (string, error)
	FocusWindow(id string) error
	FocusDir(direction string) error
	CloseWindow(id string) error
	MoveWindow(id string, direction string) error
	ResizeWindow(id string, width, height int) error
	ToggleFloating(id string) error
	SetFullscreen(id string, state bool) error
	SetMaximized(id string, state bool) error
	PinWindow(id string, state bool) error

	ToggleGroup(id string) error
	GroupNav(direction string) error
	SetLayoutProperty(id string, key, value string) error

	MoveWindowPixel(id string, x, y int) error

	ListWorkspaces() ([]Workspace, error)
	SwitchWorkspace(id string) error
	MoveToWorkspace(windowID, workspaceID string) error
	MoveToWorkspaceSilent(windowID, workspaceID string) error
	ToggleSpecialWorkspace(name string) error

	ListMonitors() ([]Monitor, error)
	FocusMonitor(id string) error
	MoveToMonitor(windowID, monitorID string) error

	SetLayout(name string) error

	GetConfig(key string) (interface{}, error)
	SetConfig(key string, value interface{}) error
	BatchConfig(configs map[string]interface{}) error
	ReloadConfig() error
	GetAnimations() (interface{}, error)
	GetCursorPosition() (int, int, error)

	BindKey(mods, key, command string) error
	UnbindKey(mods, key string) error

	Execute(command string) error
	Exit() error

	Subscribe() (<-chan Event, error)
}

type Monitor struct {
	ID        string
	Name      string
	Width     int
	Height    int
	Refresh   float64
	Active    bool
	Workspace string
}

var (
	ErrNotSupported = fmt.Errorf("feature not supported on this compositor")
)
