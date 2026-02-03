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

func (c *StateCache) AddWindow(w Window) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.windows = append(c.windows, w)
}

func (c *StateCache) RemoveWindow(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	newWindows := make([]Window, 0, len(c.windows))
	for _, w := range c.windows {
		if w.ID != id {
			newWindows = append(newWindows, w)
		}
	}
	c.windows = newWindows
}

func (c *StateCache) UpdateWindowTitle(id, title string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, w := range c.windows {
		if w.ID == id {
			c.windows[i].Title = title
			break
		}
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
