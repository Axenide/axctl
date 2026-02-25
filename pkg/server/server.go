package server

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"

	"axctl/pkg/ipc"
)

type Server struct {
	compositor ipc.Compositor
	socketPath string
	cache      *ipc.StateCache
	clients    map[net.Conn]struct{}
	clientsMu  sync.RWMutex
	idleMgr    *IdleManager
}

func New(c ipc.Compositor, path string) *Server {
	idleMgr, err := NewIdleManager()
	if err != nil {
		fmt.Printf("Warning: Failed to initialize Wayland Idle Manager: %v\n", err)
	}

	s := &Server{
		compositor: c,
		socketPath: path,
		cache:      ipc.NewStateCache(),
		clients:    make(map[net.Conn]struct{}),
		idleMgr:    idleMgr,
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
				s.broadcastEvent("Event.WindowCreated", e.Window)
			}
		case ipc.EventWindowClosed:
			if id, ok := e.Payload["address"].(string); ok {
				// Hyprland: uses "address" key with hex string
				s.cache.RemoveWindow(id)
				s.broadcastEvent("Event.WindowClosed", map[string]string{"ID": id})
			} else if id, ok := e.Payload["id"].(string); ok {
				// Niri/MangoWC: normalized string IDs
				s.cache.RemoveWindow(id)
				s.broadcastEvent("Event.WindowClosed", map[string]string{"ID": id})
			} else if id, ok := e.Payload["id"].(int); ok {
				// Fallback: legacy int IDs
				strID := fmt.Sprintf("%d", id)
				s.cache.RemoveWindow(strID)
				s.broadcastEvent("Event.WindowClosed", map[string]string{"ID": strID})
			}
		case ipc.EventWindowFocused:
			// Track window in cache if not already present (helps MangoWC accumulate windows)
			if e.Window != nil && e.Window.ID != "" {
				existing := s.cache.GetWindows()
				found := false
				for _, w := range existing {
					if w.ID == e.Window.ID {
						found = true
						break
					}
				}
				if !found {
					s.cache.AddWindow(*e.Window)
				}
			}
			if class, ok := e.Payload["class"].(string); ok {
				if title, ok := e.Payload["title"].(string); ok {
					s.broadcastEvent("Event.WindowFocused", map[string]string{"Class": class, "Title": title})
				}
			}
		case ipc.EventWindowTitleChanged:
			var id string
			if addr, ok := e.Payload["address"].(string); ok {
				id = addr
			} else if idStr, ok := e.Payload["id"].(string); ok {
				id = idStr
			} else if idInt, ok := e.Payload["id"].(int); ok {
				id = fmt.Sprintf("%d", idInt)
			}
			if id != "" {
				if title, ok := e.Payload["title"].(string); ok {
					s.cache.UpdateWindowTitle(id, title)
				}
			}
			s.broadcastEvent("Event.WindowTitleChanged", e.Payload)
		case ipc.EventWorkspaceChanged:
			s.initCache()
			if name, ok := e.Payload["name"].(string); ok {
				s.broadcastEvent("Event.WorkspaceChanged", map[string]string{"Name": name})
			} else {
				s.broadcastEvent("Event.WorkspaceChanged", e.Payload)
			}
		case ipc.EventWindowMoved:
			var id string
			if addr, ok := e.Payload["address"].(string); ok {
				id = addr
			} else if idStr, ok := e.Payload["id"].(string); ok {
				id = idStr
			} else if idInt, ok := e.Payload["id"].(int); ok {
				id = fmt.Sprintf("%d", idInt)
			}
			if id != "" {
				if ws, ok := e.Payload["workspace"].(string); ok {
					monitor, _ := e.Payload["monitor"].(string)
					s.cache.UpdateWindowWorkspace(id, ws, monitor)
					s.broadcastEvent("Event.WindowMoved", map[string]string{"ID": id, "WorkspaceID": ws})
				}
			}
		case ipc.EventMonitorChanged:
			s.initCache()
			s.broadcastEvent("Event.MonitorChanged", e.Payload)
		case ipc.EventConfigReloaded:
			s.initCache()
			s.broadcastEvent("Event.ConfigReloaded", nil)
		case ipc.EventFullscreenChanged:
			var id string
			if addr, ok := e.Payload["address"].(string); ok {
				id = addr
			} else if idStr, ok := e.Payload["id"].(string); ok {
				id = idStr
			} else if idInt, ok := e.Payload["id"].(int); ok {
				id = fmt.Sprintf("%d", idInt)
			}
			if id != "" {
				if fs, ok := e.Payload["fullscreen"].(bool); ok {
					s.cache.UpdateWindowState(id, fs)
				} else if fsStr, ok := e.Payload["fullscreen"].(string); ok {
					s.cache.UpdateWindowState(id, fsStr == "true" || fsStr == "1")
				} else if fsInt, ok := e.Payload["fullscreen"].(int); ok {
					s.cache.UpdateWindowState(id, fsInt == 1)
				}
			}
			s.broadcastEvent("Event.FullscreenChanged", e.Payload)
		case ipc.EventFocusedMonitorChanged:
			s.broadcastEvent("Event.FocusedMonitorChanged", e.Payload)
		default:
			s.initCache()
			s.broadcastEvent("Event.CacheRefreshed", nil)
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

func (s *Server) resolveID(id string) (string, error) {
	if id != "" {
		return id, nil
	}
	return s.compositor.ActiveWindow()
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	s.clientsMu.Lock()
	s.clients[conn] = struct{}{}
	s.clientsMu.Unlock()

	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, conn)
		s.clientsMu.Unlock()
	}()

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
		case "Window.Active":
			var activeID string
			activeID, err = s.compositor.ActiveWindow()
			if err == nil {
				result = map[string]string{"id": activeID}
			}
		case "Window.Focus":
			var p struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.compositor.FocusWindow(p.ID)
		case "Window.FocusDir":
			var p struct {
				Direction string `json:"direction"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.compositor.FocusDir(p.Direction)
		case "Window.Close":
			var p struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			id, _ := s.resolveID(p.ID)
			err = s.compositor.CloseWindow(id)
		case "Window.Move":
			var p struct {
				ID        string `json:"id"`
				Direction string `json:"direction"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			id, _ := s.resolveID(p.ID)
			err = s.compositor.MoveWindow(id, p.Direction)
		case "Window.Resize":
			var p struct {
				ID     string `json:"id"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			id, _ := s.resolveID(p.ID)
			err = s.compositor.ResizeWindow(id, p.Width, p.Height)
		case "Window.ToggleFloating":
			var p struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			id, _ := s.resolveID(p.ID)
			err = s.compositor.ToggleFloating(id)
		case "Window.Fullscreen":
			var p struct {
				ID    string `json:"id"`
				State bool   `json:"state"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			id, _ := s.resolveID(p.ID)
			err = s.compositor.SetFullscreen(id, p.State)
		case "Window.Maximize":
			var p struct {
				ID    string `json:"id"`
				State bool   `json:"state"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			id, _ := s.resolveID(p.ID)
			err = s.compositor.SetMaximized(id, p.State)
		case "Window.Pin":
			var p struct {
				ID    string `json:"id"`
				State bool   `json:"state"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			id, _ := s.resolveID(p.ID)
			err = s.compositor.PinWindow(id, p.State)
		case "Window.ToggleGroup":
			var p struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			id, _ := s.resolveID(p.ID)
			err = s.compositor.ToggleGroup(id)
		case "Window.GroupNav":
			var p struct {
				Direction string `json:"direction"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.compositor.GroupNav(p.Direction)
		case "Window.LayoutProp":
			var p struct {
				ID    string `json:"id"`
				Key   string `json:"key"`
				Value string `json:"value"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			id, _ := s.resolveID(p.ID)
			err = s.compositor.SetLayoutProperty(id, p.Key, p.Value)
		case "Window.MovePixel":
			var p struct {
				ID string `json:"id"`
				X  int    `json:"x"`
				Y  int    `json:"y"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			id, _ := s.resolveID(p.ID)
			err = s.compositor.MoveWindowPixel(id, p.X, p.Y)
		case "Window.MoveToWorkspaceSilent":
			var p struct {
				WindowID    string `json:"window_id"`
				WorkspaceID string `json:"workspace_id"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			id, _ := s.resolveID(p.WindowID)
			err = s.compositor.MoveToWorkspaceSilent(id, p.WorkspaceID)

		case "Workspace.List":
			result = s.cache.GetWorkspaces()
		case "Workspace.Active":
			var ws *ipc.Workspace
			ws, err = s.compositor.ActiveWorkspace()
			if err == nil {
				result = ws
			}
		case "Workspace.Switch":
			var p struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.compositor.SwitchWorkspace(p.ID)
		case "Workspace.MoveTo":
			var p struct {
				WindowID    string `json:"window_id"`
				WorkspaceID string `json:"workspace_id"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			id, _ := s.resolveID(p.WindowID)
			err = s.compositor.MoveToWorkspace(id, p.WorkspaceID)
		case "Workspace.ToggleSpecial":
			var p struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.compositor.ToggleSpecialWorkspace(p.Name)

		case "Monitor.List":
			result = s.cache.GetMonitors()
		case "Monitor.Focus":
			var p struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.compositor.FocusMonitor(p.ID)
		case "Monitor.MoveTo":
			var p struct {
				WindowID  string `json:"window_id"`
				MonitorID string `json:"monitor_id"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			id, _ := s.resolveID(p.WindowID)
			err = s.compositor.MoveToMonitor(id, p.MonitorID)
		case "Monitor.SetDpms":
			var p struct {
				MonitorID string `json:"monitor_id"`
				On        bool   `json:"on"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.compositor.SetDpms(p.MonitorID, p.On)

		case "Layout.Set":
			var p struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.compositor.SetLayout(p.Name)

		case "Config.Get":
			var p struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			result, err = s.compositor.GetConfig(p.Key)
		case "Config.Set":
			var p struct {
				Key   string      `json:"key"`
				Value interface{} `json:"value"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.compositor.SetConfig(p.Key, p.Value)
		case "Config.Batch":
			var p struct {
				Configs map[string]interface{} `json:"configs"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.compositor.BatchConfig(p.Configs)
		case "Config.Reload":
			err = s.compositor.ReloadConfig()
		case "Config.GetAnimations":
			result, err = s.compositor.GetAnimations()
		case "Config.BindKey":
			var p struct {
				Mods    string `json:"mods"`
				Key     string `json:"key"`
				Command string `json:"command"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.compositor.BindKey(p.Mods, p.Key, p.Command)
		case "Config.UnbindKey":
			var p struct {
				Mods string `json:"mods"`
				Key  string `json:"key"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.compositor.UnbindKey(p.Mods, p.Key)

		case "System.Execute":
			var p struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.compositor.Execute(p.Command)
		case "System.GetCursorPosition":
			var x, y int
			x, y, err = s.compositor.GetCursorPosition()
			if err == nil {
				result = map[string]int{"x": x, "y": y}
			}
		case "System.IdleInhibit":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			var p struct {
				On bool `json:"on"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.idleMgr.Inhibit(p.On)
		case "System.IdleWait":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			var p struct {
				TimeoutMs uint32 `json:"timeout_ms"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.idleMgr.WaitIdle(p.TimeoutMs)
		case "System.ResumeWait":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			var p struct {
				TimeoutMs uint32 `json:"timeout_ms"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.idleMgr.WaitResume(p.TimeoutMs)
		case "System.InputIdleWait":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			var p struct {
				TimeoutMs uint32 `json:"timeout_ms"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.idleMgr.WaitInputIdle(p.TimeoutMs)
		case "System.InputResumeWait":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			var p struct {
				TimeoutMs uint32 `json:"timeout_ms"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.idleMgr.WaitInputResume(p.TimeoutMs)
		case "System.IsIdle":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			var p struct {
				TimeoutMs uint32 `json:"timeout_ms"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			isIdle, e := s.idleMgr.IsIdle(p.TimeoutMs)
			err = e
			if err == nil {
				if isIdle {
					result = "true"
				} else {
					result = "false"
				}
			}
		case "System.IsInhibited":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			if s.idleMgr.IsInhibited() {
				result = "true"
			} else {
				result = "false"
			}
		case "System.IsInputIdle":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			var p struct {
				TimeoutMs uint32 `json:"timeout_ms"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			isIdle, e := s.idleMgr.IsInputIdle(p.TimeoutMs)
			err = e
			if err == nil {
				if isIdle {
					result = "true"
				} else {
					result = "false"
				}
			}
		case "System.Exit":
			err = s.compositor.Exit()
		case "System.SwitchKeyboardLayout":
			var p struct {
				Action string `json:"action"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if p.Action == "" {
				p.Action = "next"
			}
			err = s.compositor.SwitchKeyboardLayout(p.Action)
		case "System.SetKeyboardLayouts":
			var p struct {
				Layouts  string `json:"layouts"`
				Variants string `json:"variants"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.compositor.SetKeyboardLayouts(p.Layouts, p.Variants)

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

type Notification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

func (s *Server) broadcastEvent(method string, params interface{}) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	if len(s.clients) == 0 {
		return
	}

	notif := Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return
	}
	data = append(data, '\n')

	for conn := range s.clients {
		go func(c net.Conn) {
			c.Write(data)
		}(conn)
	}
}
