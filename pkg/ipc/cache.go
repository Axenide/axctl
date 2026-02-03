package ipc

import (
	"sync"
)

type StateCache struct {
	mu         sync.RWMutex
	windows    []Window
	workspaces []Workspace
	monitors   []Monitor
}

func NewStateCache() *StateCache {
	return &StateCache{
		windows:    []Window{},
		workspaces: []Workspace{},
		monitors:   []Monitor{},
	}
}

func (c *StateCache) SetWindows(w []Window) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.windows = w
}

func (c *StateCache) GetWindows() []Window {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.windows
}

func (c *StateCache) SetWorkspaces(w []Workspace) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.workspaces = w
}

func (c *StateCache) GetWorkspaces() []Workspace {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.workspaces
}

func (c *StateCache) SetMonitors(m []Monitor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.monitors = m
}

func (c *StateCache) GetMonitors() []Monitor {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.monitors
}
