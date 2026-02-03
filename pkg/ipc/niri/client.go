package niri

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"axctl/pkg/ipc"
)

type Niri struct {
	socketPath string
	mu         sync.Mutex
	conn       net.Conn
}

func New() (*Niri, error) {
	path := os.Getenv("NIRI_SOCKET")
	if path == "" {
		return nil, fmt.Errorf("NIRI_SOCKET not set")
	}
	return &Niri{socketPath: path}, nil
}

func (n *Niri) getConnection() (net.Conn, error) {
	if n.conn != nil {
		return n.conn, nil
	}
	conn, err := net.Dial("unix", n.socketPath)
	if err != nil {
		return nil, err
	}
	n.conn = conn
	return conn, nil
}

func (n *Niri) request(req interface{}, resp interface{}) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	conn, err := n.getConnection()
	if err != nil {
		return err
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		n.conn.Close()
		n.conn = nil
		return err
	}

	var reply struct {
		Reply struct {
			Ok  json.RawMessage `json:"Ok"`
			Err json.RawMessage `json:"Err"`
		} `json:"Reply"`
	}

	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		n.conn.Close()
		n.conn = nil
		return err
	}

	if len(reply.Reply.Err) > 0 {
		return fmt.Errorf("niri error: %s", string(reply.Reply.Err))
	}

	if resp != nil {
		return json.Unmarshal(reply.Reply.Ok, resp)
	}

	return nil
}

func (n *Niri) ListWindows() ([]ipc.Window, error) {
	var niriWindows []struct {
		ID           int     `json:"id"`
		Title        *string `json:"title"`
		AppID        *string `json:"app_id"`
		WorkspaceID  *int    `json:"workspace_id"`
		IsFloating   bool    `json:"is_floating"`
		IsFullscreen bool    `json:"is_fullscreen"`
	}

	err := n.request("Windows", &niriWindows)
	if err != nil {
		return nil, err
	}

	windows := make([]ipc.Window, len(niriWindows))
	for i, w := range niriWindows {
		title := ""
		if w.Title != nil {
			title = *w.Title
		}
		class := ""
		if w.AppID != nil {
			class = *w.AppID
		}
		wsID := ""
		if w.WorkspaceID != nil {
			wsID = fmt.Sprintf("%d", *w.WorkspaceID)
		}

		windows[i] = ipc.Window{
			ID:          fmt.Sprintf("%d", w.ID),
			Title:       title,
			Class:       class,
			WorkspaceID: wsID,
			Floating:    w.IsFloating,
			Fullscreen:  w.IsFullscreen,
		}
	}
	return windows, nil
}

func (n *Niri) FocusWindow(id string) error {
	var idInt int
	if _, err := fmt.Sscanf(id, "%d", &idInt); err != nil {
		return err
	}
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{
			"FocusWindow": map[string]interface{}{"id": idInt},
		},
	}, nil)
}

func (n *Niri) FocusDirection(direction string) error {
	action := ""
	switch direction {
	case "l":
		action = "FocusColumnLeft"
	case "r":
		action = "FocusColumnRight"
	case "u":
		action = "FocusWindowUp"
	case "d":
		action = "FocusWindowDown"
	default:
		return fmt.Errorf("invalid direction")
	}
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{action: map[string]interface{}{}},
	}, nil)
}

func (n *Niri) CloseWindow(id string) error {
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{"CloseWindow": map[string]interface{}{}},
	}, nil)
}

func (n *Niri) MoveWindow(id string, direction string) error {
	action := ""
	switch direction {
	case "l":
		action = "MoveColumnLeft"
	case "r":
		action = "MoveColumnRight"
	case "u":
		action = "MoveWindowUp"
	case "d":
		action = "MoveWindowDown"
	default:
		return fmt.Errorf("invalid direction")
	}
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{action: map[string]interface{}{}},
	}, nil)
}

func (n *Niri) ResizeWindow(id string, width, height int) error {
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{
			"SetWindowWidth": map[string]interface{}{
				"width": map[string]interface{}{"Fixed": width},
			},
		},
	}, nil)
}

func (n *Niri) ToggleFloating(id string) error {
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{"ToggleWindowFloating": map[string]interface{}{}},
	}, nil)
}

func (n *Niri) SetFullscreen(id string, state bool) error {
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{"FullscreenWindow": map[string]interface{}{}},
	}, nil)
}

func (n *Niri) ListWorkspaces() ([]ipc.Workspace, error) {
	var niriWorkspaces []struct {
		ID       int     `json:"id"`
		Name     *string `json:"name"`
		Output   *string `json:"output"`
		IsActive bool    `json:"is_active"`
	}

	err := n.request("Workspaces", &niriWorkspaces)
	if err != nil {
		return nil, err
	}

	res := make([]ipc.Workspace, len(niriWorkspaces))
	for i, w := range niriWorkspaces {
		name := ""
		if w.Name != nil {
			name = *w.Name
		}
		output := ""
		if w.Output != nil {
			output = *w.Output
		}
		res[i] = ipc.Workspace{
			ID:        fmt.Sprintf("%d", w.ID),
			Name:      name,
			MonitorID: output,
			Active:    w.IsActive,
		}
	}
	return res, nil
}

func (n *Niri) SwitchWorkspace(id string) error {
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{
			"FocusWorkspace": map[string]interface{}{
				"reference": map[string]interface{}{"Name": id},
			},
		},
	}, nil)
}

func (n *Niri) MoveToWorkspace(windowID, workspaceID string) error {
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{
			"MoveWindowToWorkspace": map[string]interface{}{
				"reference": map[string]interface{}{"Name": workspaceID},
			},
		},
	}, nil)
}

func (n *Niri) ListMonitors() ([]ipc.Monitor, error) {
	var niriOutputs []struct {
		Name   string `json:"name"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	err := n.request("Outputs", &niriOutputs)
	if err != nil {
		return nil, err
	}
	res := make([]ipc.Monitor, len(niriOutputs))
	for i, o := range niriOutputs {
		res[i] = ipc.Monitor{
			ID:     o.Name,
			Name:   o.Name,
			Width:  o.Width,
			Height: o.Height,
		}
	}
	return res, nil
}

func (n *Niri) FocusMonitor(id string) error {
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{"FocusMonitor": map[string]interface{}{"name": id}},
	}, nil)
}

func (n *Niri) MoveToMonitor(windowID, monitorID string) error {
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{"MoveWindowToMonitor": map[string]interface{}{"name": monitorID}},
	}, nil)
}

func (n *Niri) SetLayout(name string) error {
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{"SwitchLayout": map[string]interface{}{"name": name}},
	}, nil)
}

func (n *Niri) SetConfig(key string, value interface{}) error {
	return ipc.ErrNotSupported
}

func (n *Niri) ReloadConfig() error {
	return n.request("LoadConfigFile", nil)
}

func (n *Niri) Execute(command string) error {
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{
			"Spawn": map[string]interface{}{"command": []string{"sh", "-c", command}},
		},
	}, nil)
}

func (n *Niri) Exit() error {
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{"Quit": map[string]interface{}{}},
	}, nil)
}

func (n *Niri) Subscribe() (<-chan ipc.Event, error) {
	conn, err := net.Dial("unix", n.socketPath)
	if err != nil {
		return nil, err
	}

	if err := json.NewEncoder(conn).Encode("EventStream"); err != nil {
		conn.Close()
		return nil, err
	}

	ch := make(chan ipc.Event)
	go func() {
		defer conn.Close()
		defer close(ch)
		dec := json.NewDecoder(conn)
		for {
			var eventWrapper struct {
				Event map[string]json.RawMessage `json:"Event"`
			}
			if err := dec.Decode(&eventWrapper); err != nil {
				break
			}

			event := ipc.Event{
				Timestamp: time.Now().Unix(),
				Payload:   make(map[string]interface{}),
			}

			for name, data := range eventWrapper.Event {
				switch name {
				case "WorkspacesChanged":
					event.Type = ipc.EventWorkspaceChanged
				case "WorkspaceActivated":
					event.Type = ipc.EventWorkspaceChanged
					var d struct {
						ID int `json:"id"`
					}
					json.Unmarshal(data, &d)
					event.Payload["id"] = d.ID
				case "WindowOpened":
					event.Type = ipc.EventWindowCreated
					var d struct {
						Window struct {
							ID    int     `json:"id"`
							Title *string `json:"title"`
						} `json:"window"`
					}
					json.Unmarshal(data, &d)
					title := ""
					if d.Window.Title != nil {
						title = *d.Window.Title
					}
					event.Window = &ipc.Window{
						ID:    fmt.Sprintf("%d", d.Window.ID),
						Title: title,
					}
				case "WindowClosed":
					event.Type = ipc.EventWindowClosed
					var d struct {
						ID int `json:"id"`
					}
					json.Unmarshal(data, &d)
					event.Payload["id"] = d.ID
				case "WindowFocused":
					event.Type = ipc.EventWindowFocused
					var d struct {
						ID *int `json:"id"`
					}
					json.Unmarshal(data, &d)
					if d.ID != nil {
						event.Payload["id"] = *d.ID
					}
				}
			}

			if event.Type != "" {
				ch <- event
			}
		}
	}()

	return ch, nil
}
