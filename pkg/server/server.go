package server

import (
	"encoding/json"
	"fmt"
	"net"
	"os"

	"axctl/pkg/ipc"
)

type Server struct {
	compositor ipc.Compositor
	socketPath string
	cache      *ipc.StateCache
}

func New(c ipc.Compositor, path string) *Server {
	s := &Server{
		compositor: c,
		socketPath: path,
		cache:      ipc.NewStateCache(),
	}
	s.initCache()
	go s.watchEvents()
	return s
}

func (s *Server) initCache() {
	w, err := s.compositor.ListWindows()
	if err == nil {
		fmt.Printf("[Server] Cached %d windows\n", len(w))
		s.cache.SetWindows(w)
	} else {
		fmt.Printf("[Server] Error caching windows: %v\n", err)
	}

	ws, err := s.compositor.ListWorkspaces()
	if err == nil {
		fmt.Printf("[Server] Cached %d workspaces\n", len(ws))
		s.cache.SetWorkspaces(ws)
	}

	m, err := s.compositor.ListMonitors()
	if err == nil {
		fmt.Printf("[Server] Cached %d monitors\n", len(m))
		s.cache.SetMonitors(m)
	}
}

func (s *Server) watchEvents() {
	events, err := s.compositor.Subscribe()
	if err != nil {
		fmt.Printf("[Server] Error subscribing to events: %v\n", err)
		return
	}

	for e := range events {
		switch e.Type {
		case ipc.EventWindowCreated:
			if e.Window != nil {
				s.cache.AddWindow(*e.Window)
			}
		case ipc.EventWindowClosed:
			if id, ok := e.Payload["address"].(string); ok {
				s.cache.RemoveWindow(id)
			} else if id, ok := e.Payload["id"].(int); ok {
				s.cache.RemoveWindow(fmt.Sprintf("%d", id))
			}
		case ipc.EventWindowFocused:
		default:
			s.initCache()
		}
	}
}

type Request struct {
	ID     interface{}     `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type Response struct {
	ID     interface{} `json:"id"`
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

func (s *Server) Start() error {
	_ = os.Remove(s.socketPath)
	l, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			continue
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	for {
		var req Request
		if err := dec.Decode(&req); err != nil {
			return
		}

		fmt.Printf("[Server] Request: %s\n", req.Method)
		resp := Response{ID: req.ID}

		var err error
		var result interface{}

		switch req.Method {
		case "Window.List":
			result = s.cache.GetWindows()
		case "Window.Focus":
			var p struct {
				ID string `json:"id"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.FocusWindow(p.ID)
		case "Window.FocusDir":
			var p struct {
				Direction string `json:"direction"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.FocusDirection(p.Direction)
		case "Window.Close":
			var p struct {
				ID string `json:"id"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.CloseWindow(p.ID)
		case "Window.Move":
			var p struct {
				ID        string `json:"id"`
				Direction string `json:"direction"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.MoveWindow(p.ID, p.Direction)
		case "Window.Resize":
			var p struct {
				ID     string `json:"id"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.ResizeWindow(p.ID, p.Width, p.Height)
		case "Window.ToggleFloating":
			var p struct {
				ID string `json:"id"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.ToggleFloating(p.ID)
		case "Window.Fullscreen":
			var p struct {
				ID    string `json:"id"`
				State bool   `json:"state"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.SetFullscreen(p.ID, p.State)
		case "Window.Maximize":
			var p struct {
				ID    string `json:"id"`
				State bool   `json:"state"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.SetMaximized(p.ID, p.State)
		case "Window.Pin":
			var p struct {
				ID    string `json:"id"`
				State bool   `json:"state"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.PinWindow(p.ID, p.State)

		case "Window.ToggleGroup":
			var p struct {
				ID string `json:"id"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.ToggleGroup(p.ID)
		case "Window.GroupNav":
			var p struct {
				Direction string `json:"direction"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.GroupNavigation(p.Direction)
		case "Window.LayoutProp":
			var p struct {
				ID    string `json:"id"`
				Key   string `json:"key"`
				Value string `json:"value"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.SetLayoutProperty(p.ID, p.Key, p.Value)

		case "Workspace.List":
			result = s.cache.GetWorkspaces()
		case "Workspace.Switch":
			var p struct {
				ID string `json:"id"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.SwitchWorkspace(p.ID)
		case "Workspace.MoveTo":
			var p struct {
				WindowID    string `json:"window_id"`
				WorkspaceID string `json:"workspace_id"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.MoveToWorkspace(p.WindowID, p.WorkspaceID)

		case "Monitor.List":
			result = s.cache.GetMonitors()
		case "Monitor.Focus":
			var p struct {
				ID string `json:"id"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.FocusMonitor(p.ID)
		case "Monitor.MoveTo":
			var p struct {
				WindowID  string `json:"window_id"`
				MonitorID string `json:"monitor_id"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.MoveToMonitor(p.WindowID, p.MonitorID)

		case "Layout.Set":
			var p struct {
				Name string `json:"name"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.SetLayout(p.Name)

		case "Config.Set":
			var p struct {
				Key   string      `json:"key"`
				Value interface{} `json:"value"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.SetConfig(p.Key, p.Value)
		case "Config.Reload":
			err = s.compositor.ReloadConfig()

		case "System.Execute":
			var p struct {
				Command string `json:"command"`
			}
			json.Unmarshal(req.Params, &p)
			err = s.compositor.Execute(p.Command)
		case "System.Exit":
			err = s.compositor.Exit()

		default:
			resp.Error = "method not found"
		}

		if err != nil {
			resp.Error = err.Error()
		} else if result != nil {
			resp.Result = result
		} else {
			resp.Result = "ok"
		}

		enc.Encode(resp)
	}
}
