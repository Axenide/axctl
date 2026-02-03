package mock

import (
	"encoding/json"
	"sync"

	"axctl/pkg/ipc"
)

// Compositor is a mock implementation of the ipc.Compositor interface.
// It supports simulating various compositor behaviors for testing.
type Compositor struct {
	mu sync.RWMutex

	// Storage for mock data
	windows    []ipc.Window
	workspaces []ipc.Workspace
	events     chan ipc.Event

	// Configurable responses
	listWindowsErr     error
	focusWindowErr     error
	closeWindowErr     error
	listWorkspacesErr  error
	switchWorkspaceErr error
	subscribeErr       error

	// Call tracking for verification
	calls struct {
		listWindows     int
		focusWindow     []string
		closeWindow     []string
		listWorkspaces  int
		switchWorkspace []string
		subscribe       int
	}

	// Subscription state
	subscribed bool
	eventChan  chan ipc.Event
	cancelChan chan struct{}
	eventWg    sync.WaitGroup
}

// NewCompositor creates a new mock compositor with default values.
func NewCompositor() *Compositor {
	return &Compositor{
		windows:    []ipc.Window{},
		workspaces: []ipc.Workspace{},
		eventChan:  make(chan ipc.Event, 100), // buffered to allow queuing events
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

// ListWindows returns all currently open windows.
func (c *Compositor) ListWindows() ([]ipc.Window, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls.listWindows++

	if c.listWindowsErr != nil {
		return nil, c.listWindowsErr
	}

	// Return a copy to prevent external modification
	windows := make([]ipc.Window, len(c.windows))
	copy(windows, c.windows)
	return windows, nil
}

// FocusWindow brings the window with the given ID into focus.
func (c *Compositor) FocusWindow(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls.focusWindow = append(c.calls.focusWindow, id)

	if c.focusWindowErr != nil {
		return c.focusWindowErr
	}

	// Check if window exists
	for _, w := range c.windows {
		if w.ID == id {
			return nil
		}
	}

	return ipc.ErrWindowNotFound
}

// CloseWindow closes the window with the given ID.
func (c *Compositor) CloseWindow(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls.closeWindow = append(c.calls.closeWindow, id)

	if c.closeWindowErr != nil {
		return c.closeWindowErr
	}

	// Check if window exists and remove it
	for i, w := range c.windows {
		if w.ID == id {
			c.windows = append(c.windows[:i], c.windows[i+1:]...)
			return nil
		}
	}

	return ipc.ErrWindowNotFound
}

// ListWorkspaces returns all workspaces known to the compositor.
func (c *Compositor) ListWorkspaces() ([]ipc.Workspace, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls.listWorkspaces++

	if c.listWorkspacesErr != nil {
		return nil, c.listWorkspacesErr
	}

	// Return a copy to prevent external modification
	workspaces := make([]ipc.Workspace, len(c.workspaces))
	copy(workspaces, c.workspaces)
	return workspaces, nil
}

// SwitchWorkspace switches to the workspace with the given ID.
func (c *Compositor) SwitchWorkspace(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls.switchWorkspace = append(c.calls.switchWorkspace, id)

	if c.switchWorkspaceErr != nil {
		return c.switchWorkspaceErr
	}

	// Check if workspace exists
	for _, ws := range c.workspaces {
		if ws.ID == id {
			return nil
		}
	}

	return ipc.ErrWorkspaceNotFound
}

// Subscribe returns a channel that emits compositor events.
func (c *Compositor) Subscribe() (<-chan ipc.Event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls.subscribe++

	if c.subscribeErr != nil {
		return nil, c.subscribeErr
	}

	if c.subscribed {
		// Return a new channel for this subscription
		ch := make(chan ipc.Event, 100)
		return ch, nil
	}

	c.subscribed = true

	// Start goroutine to forward events
	c.eventWg.Add(1)
	go func() {
		defer c.eventWg.Done()
		for {
			select {
			case <-c.eventChan:
				// Events are directly received from channel by subscribers
				// No forwarding needed; channel is passed directly to caller
			case <-c.cancelChan:
				return
			}
		}
	}()

	return c.eventChan, nil
}

// ============================================================================
// Helper Methods for Testing
// ============================================================================

// AddWindow adds a window to the mock compositor's state.
func (c *Compositor) AddWindow(w ipc.Window) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.windows = append(c.windows, w)
}

// AddWorkspace adds a workspace to the mock compositor's state.
func (c *Compositor) AddWorkspace(ws ipc.Workspace) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.workspaces = append(c.workspaces, ws)
}

// SetListWindowsError sets the error to return from ListWindows.
func (c *Compositor) SetListWindowsError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.listWindowsErr = err
}

// SetFocusWindowError sets the error to return from FocusWindow.
func (c *Compositor) SetFocusWindowError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.focusWindowErr = err
}

// SetCloseWindowError sets the error to return from CloseWindow.
func (c *Compositor) SetCloseWindowError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeWindowErr = err
}

// SetListWorkspacesError sets the error to return from ListWorkspaces.
func (c *Compositor) SetListWorkspacesError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.listWorkspacesErr = err
}

// SetSwitchWorkspaceError sets the error to return from SwitchWorkspace.
func (c *Compositor) SetSwitchWorkspaceError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.switchWorkspaceErr = err
}

// SetSubscribeError sets the error to return from Subscribe.
func (c *Compositor) SetSubscribeError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscribeErr = err
}

// EmitEvent injects an event into the subscription channel.
// This is used to simulate compositor events during testing.
func (c *Compositor) EmitEvent(evt ipc.Event) {
	c.mu.RLock()
	ch := c.eventChan
	c.mu.RUnlock()

	if ch != nil {
		select {
		case ch <- evt:
		case <-c.cancelChan:
			// Subscription was cancelled
		}
	}
}

// Close closes the event channel and stops the subscription.
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

// ============================================================================
// Call Tracking Methods
// ============================================================================

// ListWindowsCalls returns the number of times ListWindows was called.
func (c *Compositor) ListWindowsCalls() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.calls.listWindows
}

// FocusWindowCalls returns the list of window IDs passed to FocusWindow.
func (c *Compositor) FocusWindowCalls() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	calls := make([]string, len(c.calls.focusWindow))
	copy(calls, c.calls.focusWindow)
	return calls
}

// CloseWindowCalls returns the list of window IDs passed to CloseWindow.
func (c *Compositor) CloseWindowCalls() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	calls := make([]string, len(c.calls.closeWindow))
	copy(calls, c.calls.closeWindow)
	return calls
}

// ListWorkspacesCalls returns the number of times ListWorkspaces was called.
func (c *Compositor) ListWorkspacesCalls() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.calls.listWorkspaces
}

// SwitchWorkspaceCalls returns the list of workspace IDs passed to SwitchWorkspace.
func (c *Compositor) SwitchWorkspaceCalls() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	calls := make([]string, len(c.calls.switchWorkspace))
	copy(calls, c.calls.switchWorkspace)
	return calls
}

// SubscribeCalls returns the number of times Subscribe was called.
func (c *Compositor) SubscribeCalls() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.calls.subscribe
}

// Reset clears all stored data and call history.
func (c *Compositor) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.windows = []ipc.Window{}
	c.workspaces = []ipc.Workspace{}
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

// ============================================================================
// JSON/Text Helper Functions
// ============================================================================

// WindowToJSON converts a Window to JSON bytes.
func WindowToJSON(w ipc.Window) []byte {
	data, _ := json.Marshal(w)
	return data
}

// WindowFromJSON parses JSON bytes into a Window.
func WindowFromJSON(data []byte) (*ipc.Window, error) {
	var w ipc.Window
	err := json.Unmarshal(data, &w)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// WorkspaceToJSON converts a Workspace to JSON bytes.
func WorkspaceToJSON(ws ipc.Workspace) []byte {
	data, _ := json.Marshal(ws)
	return data
}

// WorkspaceFromJSON parses JSON bytes into a Workspace.
func WorkspaceFromJSON(data []byte) (*ipc.Workspace, error) {
	var ws ipc.Workspace
	err := json.Unmarshal(data, &ws)
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

// EventToJSON converts an Event to JSON bytes.
func EventToJSON(evt ipc.Event) []byte {
	data, _ := json.Marshal(evt)
	return data
}

// EventFromJSON parses JSON bytes into an Event.
func EventFromJSON(data []byte) (*ipc.Event, error) {
	var evt ipc.Event
	err := json.Unmarshal(data, &evt)
	if err != nil {
		return nil, err
	}
	return &evt, nil
}

// EventToText converts an Event to a human-readable text format.
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
	default:
		return "UNKNOWN_EVENT"
	}
}
