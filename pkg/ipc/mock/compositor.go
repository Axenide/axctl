package mock

import (
	"encoding/json"
	"sync"

	"axctl/pkg/ipc"
)

type Compositor struct {
	mu sync.RWMutex

	windows    []ipc.Window
	workspaces []ipc.Workspace
	monitors   []ipc.Monitor
	events     chan ipc.Event

	listWindowsErr     error
	focusWindowErr     error
	closeWindowErr     error
	listWorkspacesErr  error
	switchWorkspaceErr error
	subscribeErr       error

	calls struct {
		listWindows     int
		focusWindow     []string
		closeWindow     []string
		listWorkspaces  int
		switchWorkspace []string
		subscribe       int
	}

	subscribed bool
	eventChan  chan ipc.Event
	cancelChan chan struct{}
	eventWg    sync.WaitGroup
}

func NewCompositor() *Compositor {
	return &Compositor{
		windows:    []ipc.Window{},
		workspaces: []ipc.Workspace{},
		monitors:   []ipc.Monitor{},
		eventChan:  make(chan ipc.Event, 100),
		cancelChan: make(chan struct{}),
		calls: struct {
			listWindows     int
			focusWindow     []string
			closeWindow     []string
			listWorkspaces  int
			switchWorkspace []string
			subscribe       int
		}{
			focusWindow:     []string{},
			closeWindow:     []string{},
			switchWorkspace: []string{},
		},
	}
}

func (c *Compositor) ListWindows() ([]ipc.Window, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls.listWindows++

	if c.listWindowsErr != nil {
		return nil, c.listWindowsErr
	}

	windows := make([]ipc.Window, len(c.windows))
	copy(windows, c.windows)
	return windows, nil
}

func (c *Compositor) ActiveWindow() (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, w := range c.windows {
		return w.ID, nil
	}
	return "", nil
}

func (c *Compositor) FocusWindow(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls.focusWindow = append(c.calls.focusWindow, id)

	if c.focusWindowErr != nil {
		return c.focusWindowErr
	}

	for _, w := range c.windows {
		if w.ID == id {
			return nil
		}
	}

	return ipc.ErrWindowNotFound
}

func (c *Compositor) FocusDir(direction string) error {
	return nil
}

func (c *Compositor) CloseWindow(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls.closeWindow = append(c.calls.closeWindow, id)

	if c.closeWindowErr != nil {
		return c.closeWindowErr
	}

	for i, w := range c.windows {
		if w.ID == id {
			c.windows = append(c.windows[:i], c.windows[i+1:]...)
			return nil
		}
	}

	return ipc.ErrWindowNotFound
}

func (c *Compositor) MoveWindow(id string, direction string) error {
	return nil
}

func (c *Compositor) ResizeWindow(id string, width, height int) error {
	return nil
}

func (c *Compositor) ToggleFloating(id string) error {
	return nil
}

func (c *Compositor) SetFullscreen(id string, state bool) error {
	return nil
}

func (c *Compositor) SetMaximized(id string, state bool) error {
	return nil
}

func (c *Compositor) PinWindow(id string, state bool) error {
	return nil
}

func (c *Compositor) ToggleGroup(id string) error {
	return nil
}

func (c *Compositor) GroupNav(direction string) error {
	return nil
}

func (c *Compositor) SetLayoutProperty(id string, key, value string) error {
	return nil
}

func (c *Compositor) MoveWindowPixel(id string, x, y int) error {
	return nil
}

func (c *Compositor) ListWorkspaces() ([]ipc.Workspace, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls.listWorkspaces++

	if c.listWorkspacesErr != nil {
		return nil, c.listWorkspacesErr
	}

	workspaces := make([]ipc.Workspace, len(c.workspaces))
	copy(workspaces, c.workspaces)
	return workspaces, nil
}

func (c *Compositor) ActiveWorkspace() (*ipc.Workspace, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, ws := range c.workspaces {
		if ws.Active {
			return &ws, nil
		}
	}
	if len(c.workspaces) > 0 {
		return &c.workspaces[0], nil
	}
	return nil, ipc.ErrWorkspaceNotFound
}

func (c *Compositor) SwitchWorkspace(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls.switchWorkspace = append(c.calls.switchWorkspace, id)

	if c.switchWorkspaceErr != nil {
		return c.switchWorkspaceErr
	}

	for _, ws := range c.workspaces {
		if ws.ID == id {
			return nil
		}
	}

	return ipc.ErrWorkspaceNotFound
}

func (c *Compositor) MoveToWorkspace(windowID, workspaceID string) error {
	return nil
}

func (c *Compositor) MoveToWorkspaceSilent(windowID, workspaceID string) error {
	return nil
}

func (c *Compositor) ToggleSpecialWorkspace(name string) error {
	return nil
}

func (c *Compositor) ListMonitors() ([]ipc.Monitor, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	monitors := make([]ipc.Monitor, len(c.monitors))
	copy(monitors, c.monitors)
	return monitors, nil
}

func (c *Compositor) FocusMonitor(id string) error {
	return nil
}

func (c *Compositor) MoveToMonitor(windowID, monitorID string) error {
	return nil
}

func (c *Compositor) SetDpms(monitorID string, on bool) error {
	return nil
}

func (c *Compositor) SetLayout(name string) error {
	return nil
}

func (c *Compositor) GetConfig(key string) (interface{}, error) {
	return nil, nil
}

func (c *Compositor) SetConfig(key string, value interface{}) error {
	return nil
}

func (c *Compositor) BatchConfig(configs map[string]interface{}) error {
	return nil
}

func (c *Compositor) ReloadConfig() error {
	return nil
}

func (c *Compositor) GetAnimations() (interface{}, error) {
	return nil, nil
}

func (c *Compositor) GetCursorPosition() (int, int, error) {
	return 0, 0, nil
}

func (c *Compositor) BindKey(mods, key, command string) error {
	return nil
}

func (c *Compositor) UnbindKey(mods, key string) error {
	return nil
}

func (c *Compositor) Execute(command string) error {
	return nil
}

func (c *Compositor) Exit() error {
	return nil
}

func (c *Compositor) Subscribe() (<-chan ipc.Event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls.subscribe++

	if c.subscribeErr != nil {
		return nil, c.subscribeErr
	}

	if c.subscribed {
		ch := make(chan ipc.Event, 100)
		return ch, nil
	}

	c.subscribed = true

	c.eventWg.Add(1)
	go func() {
		defer c.eventWg.Done()
		for {
			select {
			case <-c.eventChan:
			case <-c.cancelChan:
				return
			}
		}
	}()

	return c.eventChan, nil
}

func (c *Compositor) AddWindow(w ipc.Window) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.windows = append(c.windows, w)
}

func (c *Compositor) AddWorkspace(ws ipc.Workspace) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.workspaces = append(c.workspaces, ws)
}

func (c *Compositor) AddMonitor(m ipc.Monitor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.monitors = append(c.monitors, m)
}

func (c *Compositor) SetListWindowsError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.listWindowsErr = err
}

func (c *Compositor) SetFocusWindowError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.focusWindowErr = err
}

func (c *Compositor) SetCloseWindowError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeWindowErr = err
}

func (c *Compositor) SetListWorkspacesError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.listWorkspacesErr = err
}

func (c *Compositor) SetSwitchWorkspaceError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.switchWorkspaceErr = err
}

func (c *Compositor) SetSubscribeError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscribeErr = err
}

func (c *Compositor) EmitEvent(evt ipc.Event) {
	c.mu.RLock()
	ch := c.eventChan
	c.mu.RUnlock()

	if ch != nil {
		select {
		case ch <- evt:
		case <-c.cancelChan:
		}
	}
}

func (c *Compositor) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.subscribed {
		close(c.cancelChan)
		c.eventWg.Wait()
		close(c.eventChan)
		c.subscribed = false
	}
}

func (c *Compositor) ListWindowsCalls() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.calls.listWindows
}

func (c *Compositor) FocusWindowCalls() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	calls := make([]string, len(c.calls.focusWindow))
	copy(calls, c.calls.focusWindow)
	return calls
}

func (c *Compositor) CloseWindowCalls() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	calls := make([]string, len(c.calls.closeWindow))
	copy(calls, c.calls.closeWindow)
	return calls
}

func (c *Compositor) ListWorkspacesCalls() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.calls.listWorkspaces
}

func (c *Compositor) SwitchWorkspaceCalls() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	calls := make([]string, len(c.calls.switchWorkspace))
	copy(calls, c.calls.switchWorkspace)
	return calls
}

func (c *Compositor) SubscribeCalls() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.calls.subscribe
}

func (c *Compositor) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.windows = []ipc.Window{}
	c.workspaces = []ipc.Workspace{}
	c.monitors = []ipc.Monitor{}
	c.listWindowsErr = nil
	c.focusWindowErr = nil
	c.closeWindowErr = nil
	c.listWorkspacesErr = nil
	c.switchWorkspaceErr = nil
	c.subscribeErr = nil

	c.calls.listWindows = 0
	c.calls.focusWindow = []string{}
	c.calls.closeWindow = []string{}
	c.calls.listWorkspaces = 0
	c.calls.switchWorkspace = []string{}
	c.calls.subscribe = 0
}

func WindowToJSON(w ipc.Window) []byte {
	data, _ := json.Marshal(w)
	return data
}

func WindowFromJSON(data []byte) (*ipc.Window, error) {
	var w ipc.Window
	err := json.Unmarshal(data, &w)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func WorkspaceToJSON(ws ipc.Workspace) []byte {
	data, _ := json.Marshal(ws)
	return data
}

func WorkspaceFromJSON(data []byte) (*ipc.Workspace, error) {
	var ws ipc.Workspace
	err := json.Unmarshal(data, &ws)
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

func EventToJSON(evt ipc.Event) []byte {
	data, _ := json.Marshal(evt)
	return data
}

func EventFromJSON(data []byte) (*ipc.Event, error) {
	var evt ipc.Event
	err := json.Unmarshal(data, &evt)
	if err != nil {
		return nil, err
	}
	return &evt, nil
}

func EventToText(evt ipc.Event) string {
	switch evt.Type {
	case ipc.EventWindowCreated:
		if evt.Window != nil {
			return "WINDOW_CREATED: " + evt.Window.ID + " (" + evt.Window.Title + ")"
		}
		return "WINDOW_CREATED"
	case ipc.EventWindowClosed:
		if evt.Window != nil {
			return "WINDOW_CLOSED: " + evt.Window.ID
		}
		return "WINDOW_CLOSED"
	case ipc.EventWindowFocused:
		if evt.Window != nil {
			return "WINDOW_FOCUSED: " + evt.Window.ID + " (" + evt.Window.Title + ")"
		}
		return "WINDOW_FOCUSED"
	case ipc.EventWindowTitleChanged:
		if evt.Window != nil {
			return "WINDOW_TITLE_CHANGED: " + evt.Window.ID + " -> " + evt.Window.Title
		}
		return "WINDOW_TITLE_CHANGED"
	case ipc.EventWorkspaceChanged:
		if evt.Workspace != nil {
			return "WORKSPACE_CHANGED: " + evt.Workspace.ID + " (" + evt.Workspace.Name + ")"
		}
		return "WORKSPACE_CHANGED"
	case ipc.EventMonitorChanged:
		return "MONITOR_CHANGED"
	case ipc.EventConfigReloaded:
		return "CONFIG_RELOADED"
	case ipc.EventFullscreenChanged:
		return "FULLSCREEN_CHANGED"
	case ipc.EventFocusedMonitorChanged:
		return "FOCUSED_MONITOR_CHANGED"
	default:
		return "UNKNOWN_EVENT"
	}
}

func (m *Compositor) SwitchKeyboardLayout(action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil
}

func (m *Compositor) SetKeyboardLayouts(layouts string, variants string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil
}
