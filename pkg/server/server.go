package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"axctl/pkg/applications"
	"axctl/pkg/icons"
	"axctl/pkg/ipc"
	"axctl/pkg/ipc/hyprland"
	"axctl/pkg/ipc/mango"
	"axctl/pkg/ipc/niri"
	"axctl/pkg/keymon"
	"axctl/pkg/notifications"
	"axctl/pkg/tray"
)

type Server struct {
	compositor    ipc.Compositor
	socketPath    string
	cache         *ipc.StateCache
	cfgState      *ConfigState
	apps          *applications.Catalog
	iconResolver  *icons.Resolver
	notifications *notifications.Service
	tray          *tray.Service
	keyMon        *keymon.Monitor
	clients       map[net.Conn]*eventClient
	clientsMu     sync.RWMutex
	idleMgr       *IdleManager

	mu           sync.RWMutex
	overviewOpen *bool
	servicesCtx  context.Context
	servicesStop context.CancelFunc
	appRevision  uint64
}

// eventClient is a subscribed connection with its own serialized outbound
// queue. A dedicated writer goroutine drains the queue so notifications
// reach the client in the order they were broadcast; writing directly from
// ephemeral goroutines let concurrent broadcasts race and deliver stale
// state dumps after fresh ones (e.g. an inverted overview_open).
type eventClient struct {
	conn net.Conn
	out  chan []byte
}

const eventClientQueueSize = 256

func requiresCompositor(method string) bool {
	for _, prefix := range []string{"Window.", "Workspace.", "Monitor.", "Layout.", "Config.", "Overview."} {
		if strings.HasPrefix(method, prefix) {
			return true
		}
	}
	switch method {
	case "System.Execute", "System.GetCursorPosition", "System.GetCapabilities", "System.GetCompositor", "System.Exit", "System.SwitchKeyboardLayout", "System.SetKeyboardLayouts":
		return true
	default:
		return false
	}
}

func (ec *eventClient) writeLoop() {
	for data := range ec.out {
		if _, err := ec.conn.Write(data); err != nil {
			return
		}
	}
}

func New(c ipc.Compositor, path string) *Server {
	idleMgr, err := NewIdleManager()
	if err != nil {
		fmt.Printf("Warning: Failed to initialize Wayland Idle Manager: %v\n", err)
	}

	servicesCtx, servicesStop := context.WithCancel(context.Background())
	s := &Server{
		compositor:   c,
		socketPath:   path,
		cache:        ipc.NewStateCache(),
		cfgState:     NewConfigState(),
		iconResolver: icons.NewResolver(),
		keyMon:       keymon.NewMonitor(),
		clients:      make(map[net.Conn]*eventClient),
		idleMgr:      idleMgr,
		servicesCtx:  servicesCtx,
		servicesStop: servicesStop,
	}
	if apps, appsErr := applications.NewCatalog(); appsErr == nil {
		s.apps = apps
		if watchErr := apps.StartWatching(servicesCtx, func([]applications.Entry) {
			s.mu.Lock()
			s.appRevision++
			revision := s.appRevision
			s.mu.Unlock()
			s.broadcastStandaloneEvent("Event.ApplicationsChanged", map[string]uint64{"revision": revision})
		}); watchErr != nil {
			fmt.Printf("Warning: Failed to watch application entries: %v\n", watchErr)
		}
	} else {
		fmt.Printf("Warning: Failed to initialize application catalog: %v\n", appsErr)
	}
	if service, serviceErr := notifications.New(); serviceErr == nil {
		s.notifications = service
	} else {
		fmt.Printf("Warning: Failed to initialize notification service: %v\n", serviceErr)
	}
	if service, serviceErr := tray.New(); serviceErr == nil {
		s.tray = service
	} else {
		fmt.Printf("Warning: Failed to initialize tray service: %v\n", serviceErr)
	}
	if idleMgr != nil {
		idleMgr.SetIdleMonitorCallback(s.handleIdleMonitorChanged)
	}
	if s.isMango() {
		s.cfgState.Seed(ipc.ConfigUniversal{})
	}
	if c != nil {
		s.initCache()
		go s.watchEvents()
	}
	return s
}

func (s *Server) Close() {
	if s.servicesStop != nil {
		s.servicesStop()
	}
	if s.idleMgr != nil {
		_ = s.idleMgr.Close()
	}
	if s.notifications != nil {
		_ = s.notifications.Close()
	}
	if s.tray != nil {
		_ = s.tray.Close()
	}
}

func (s *Server) StartServices() {
	if s.notifications != nil {
		events, cancel := s.notifications.Subscribe()
		if err := s.notifications.Start(); err != nil {
			fmt.Printf("Warning: Notification D-Bus service: %v\n", err)
		}
		go func() {
			defer cancel()
			for {
				select {
				case <-s.servicesCtx.Done():
					return
				case event, ok := <-events:
					if !ok {
						return
					}
					method := map[string]string{
						"received":        "Event.NotificationReceived",
						"action-invoked":  "Event.NotificationActionInvoked",
						"closed":          "Event.NotificationClosed",
						"deleted":         "Event.NotificationDeleted",
						"history-cleared": "Event.NotificationHistoryCleared",
						"dnd-changed":     "Event.NotificationDNDChanged",
					}[event.Type]
					if method != "" {
						s.broadcastStandaloneEvent(method, event)
					}
				}
			}
		}()
	}
	if s.tray != nil {
		events, cancel := s.tray.Subscribe()
		if err := s.tray.Start(); err != nil {
			fmt.Printf("Warning: StatusNotifier tray service: %v\n", err)
		}
		go func() {
			defer cancel()
			for {
				select {
				case <-s.servicesCtx.Done():
					return
				case event, ok := <-events:
					if !ok {
						return
					}
					method := "Event.TrayChanged"
					if event.Type == "removed" {
						method = "Event.TrayItemRemoved"
					}
					s.broadcastStandaloneEvent(method, event)
				}
			}
		}()
	}
}

func (s *Server) initCache() {
	w, err := s.compositor.ListWindows()
	if err == nil {
		s.cache.SetWindows(w)
	}

	if activeID, err := s.compositor.ActiveWindow(); err == nil && activeID != "" {
		s.cache.MarkWindowFocused(activeID)
	}

	ws, err := s.compositor.ListWorkspaces()
	if err == nil {
		s.cache.SetWorkspaces(ws)
	}

	m, err := s.compositor.ListMonitors()
	if err == nil {
		s.cache.SetMonitors(m)
	}
}

// SeedConfigState loads the last applied config payload into the cache so
// partial updates (Config.Set, Config.KeybindsBatch) can regenerate the
// generated file. The daemon calls it after applying the TOML at startup
// and on reload.
func (s *Server) SeedConfigState(payload ipc.ConfigUniversal) {
	s.cfgState.Seed(payload)
	s.syncKeyMonitor()
}

// syncKeyMonitor re-registers the modifier-alone binds (e.g. Super_L with
// SUPER) from the cached config payload. The keymon evdev monitor is only
// used on niri, which cannot express "released alone" natively — Hyprland
// and MangoWC handle those binds themselves through their release-based
// bind variants, so nothing is registered here there.
func (s *Server) syncKeyMonitor() {
	if !s.isNiri() {
		s.keyMon.SetBinds(map[string]string{})
		return
	}
	payload, ok := s.cfgState.Current()
	if !ok {
		s.keyMon.SetBinds(map[string]string{})
		return
	}
	s.keyMon.SetBinds(keymonBindsFromPayload(payload))
}

func (s *Server) isNiri() bool {
	_, ok := s.compositor.(*niri.Niri)
	return ok
}

func (s *Server) isMango() bool {
	_, ok := s.compositor.(*mango.Mango)
	return ok
}

func isCachedAppearanceKey(key string) bool {
	var appearance ipc.ConfigAppearance
	_, ok := ipc.GetAppearanceKey(&appearance, key)
	return ok
}

// mergeKeybindsPayload applies a batch keybinds payload to the cached
// config state without touching the compositor, so partial updates keep
// the state keymon syncs from in step with runtime changes.
func (s *Server) mergeKeybindsPayload(jsonPayload string) error {
	var payload ipc.BatchKeybindsPayload
	if err := json.Unmarshal([]byte(jsonPayload), &payload); err != nil {
		return err
	}
	_, err := s.cfgState.ApplyKeybinds(payload)
	return err
}

// applyGeneratedConfig regenerates the compositor-specific generated config
// from the cached universal payload and reloads the active compositor.
func (s *Server) applyGeneratedConfig(mutate func(state *ConfigState) (ipc.ConfigUniversal, error)) error {
	payload, err := mutate(s.cfgState)
	if err != nil {
		return err
	}
	if err := NewConfigHandler(s.compositor).ApplyConfig(payload); err != nil {
		return err
	}
	s.syncKeyMonitor()
	return nil
}

func (s *Server) listLayouts() (ipc.Layouts, error) {
	items, err := s.compositor.ListLayouts()
	if err != nil {
		return ipc.Layouts{}, err
	}
	if items == nil {
		items = []ipc.Layout{}
	}
	active := ""
	source := ipc.LayoutSourceDynamic
	for i, item := range items {
		if item.Current {
			active = item.Name
			items[i] = item
		}
		if item.Source == ipc.LayoutSourceStatic {
			source = ipc.LayoutSourceStatic
		}
	}
	return ipc.Layouts{
		Items:      items,
		Active:     active,
		Compositor: s.compositorName(),
		Source:     source,
	}, nil
}

func (s *Server) currentLayout() (ipc.Layout, error) {
	items, err := s.compositor.ListLayouts()
	if err != nil {
		return ipc.Layout{}, err
	}
	for _, item := range items {
		if item.Current {
			return item, nil
		}
	}
	if len(items) > 0 {
		return items[0], nil
	}
	return ipc.Layout{}, fmt.Errorf("no layouts available")
}

func (s *Server) cycleLayout(direction int, wrap *bool) (ipc.Layout, error) {
	items, err := s.compositor.ListLayouts()
	if err != nil {
		return ipc.Layout{}, err
	}
	if len(items) == 0 {
		return ipc.Layout{}, fmt.Errorf("no layouts available")
	}
	currentIdx := -1
	for i, item := range items {
		if item.Current {
			currentIdx = i
			break
		}
	}
	doWrap := true
	if wrap != nil {
		doWrap = *wrap
	}
	nextIdx := currentIdx + direction
	if nextIdx < 0 {
		if doWrap {
			nextIdx = len(items) - 1
		} else {
			return items[currentIdx], nil
		}
	}
	if nextIdx >= len(items) {
		if doWrap {
			nextIdx = 0
		} else {
			return items[currentIdx], nil
		}
	}
	target := items[nextIdx]
	if err := s.compositor.SetLayout(target.Name); err != nil {
		return ipc.Layout{}, err
	}
	for i := range items {
		items[i].Current = items[i].Name == target.Name
	}
	return target, nil
}

func layoutExists(items []ipc.Layout, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func (s *Server) compositorName() string {
	switch s.compositor.(type) {
	case *hyprland.Hyprland:
		return "hyprland"
	case *niri.Niri:
		return "niri"
	case *mango.Mango:
		return "mango"
	default:
		return "unknown"
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
			// The event carries the workspace NAME and no monitor info,
			// which does not match the ID-based window dump (special and
			// named workspaces would never match). Replace the appended
			// entry with a fresh dump instead of trusting it.
			s.initCache()
			if e.Window != nil {
				s.broadcastEvent("Event.WindowCreated", e.Window)
			}
		case ipc.EventWindowClosed:
			if id, ok := e.Payload["address"].(string); ok {
				s.cache.RemoveWindow(id)
				s.broadcastEvent("Event.WindowClosed", map[string]string{"ID": id})
			} else if id, ok := e.Payload["id"].(string); ok {
				s.cache.RemoveWindow(id)
				s.broadcastEvent("Event.WindowClosed", map[string]string{"ID": id})
			} else if id, ok := e.Payload["id"].(int); ok {
				strID := fmt.Sprintf("%d", id)
				s.cache.RemoveWindow(strID)
				s.broadcastEvent("Event.WindowClosed", map[string]string{"ID": strID})
			}
		case ipc.EventWindowFocused:
			// Mark focus in cache BEFORE broadcasting
			if addr, ok := e.Payload["address"].(string); ok {
				s.cache.MarkWindowFocused(addr)
			}
			// Track window in cache if not already present (helps Mango accumulate windows)
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
			s.broadcastEvent("Event.WindowFocused", e.Payload)
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
			// Unfocused windows open as title-changed events; if the
			// window is not tracked yet (e.g. it opened on a workspace
			// that just emptied), add it instead of dropping it.
			if e.Window != nil && e.Window.ID != "" {
				found := false
				for _, w := range s.cache.GetWindows() {
					if w.ID == e.Window.ID {
						found = true
						break
					}
				}
				if !found {
					s.cache.AddWindow(*e.Window)
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
			// Same as window created: the event's workspace value is a
			// name, not the dump's numeric ID, so patching a single entry
			// can desync the cache. Refresh instead.
			s.initCache()
			s.broadcastEvent("Event.WindowMoved", e.Payload)
		case ipc.EventMonitorChanged:
			s.initCache()
			s.broadcastEvent("Event.MonitorChanged", e.Payload)
		case ipc.EventConfigReloaded:
			s.initCache()
			s.broadcastEvent("Event.ConfigReloaded", nil)
		case ipc.EventFullscreenChanged:
			// Fullscreen changes are not limited to the focused window
			// (clients can request fullscreen on any monitor), so a
			// targeted cache patch is unreliable. Refresh the whole cache
			// so subscribers immediately see the real state.
			s.initCache()
			s.broadcastEvent("Event.FullscreenChanged", e.Payload)
		case ipc.EventFocusedMonitorChanged:
			s.initCache()
			s.broadcastEvent("Event.FocusedMonitorChanged", e.Payload)
		case ipc.EventOverviewChanged:
			if open, ok := e.Payload["is_open"].(bool); ok {
				s.mu.Lock()
				s.overviewOpen = &open
				s.mu.Unlock()
			}
			s.broadcastEvent("Event.OverviewChanged", e.Payload)
		default:
			// Check if this is a floating mode change (has address + floating in payload)
			if addr, ok := e.Payload["address"].(string); ok {
				if floating, ok := e.Payload["floating"].(bool); ok {
					s.cache.UpdateWindowFloating(addr, floating)
					s.broadcastEvent("Event.FloatingChanged", e.Payload)
					continue
				}
			}
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
	if s.compositor == nil {
		return "", fmt.Errorf("no supported compositor is available")
	}
	return s.compositor.ActiveWindow()
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	if err := verifyPeerUID(conn); err != nil {
		fmt.Printf("[axctl] rejected connection: %v\n", err)
		return
	}
	defer func() {
		s.clientsMu.Lock()
		if ec, ok := s.clients[conn]; ok {
			close(ec.out)
			delete(s.clients, conn)
		}
		s.clientsMu.Unlock()
	}()

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	for {
		var req Request
		if err := dec.Decode(&req); err != nil {
			return
		}

		resp := Response{ID: req.ID}
		if s.compositor == nil && requiresCompositor(req.Method) {
			resp.Error = "no supported compositor is available"
			if err := enc.Encode(resp); err != nil {
				return
			}
			continue
		}

		var err error
		var result interface{}
		startSubscription := false

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
		case "Window.FakeFullscreen":
			var p struct {
				ID    string `json:"id"`
				State bool   `json:"state"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			id, _ := s.resolveID(p.ID)
			err = s.compositor.SetFakeFullscreen(id, p.State)
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

		case "Overview.Toggle":
			err = s.compositor.ToggleOverview()

		case "Layout.List":
			result, err = s.listLayouts()
		case "Layout.Set":
			var p struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if p.Name == "" {
				resp.Error = "layout name is required"
				break
			}
			if items, lerr := s.compositor.ListLayouts(); lerr == nil && len(items) > 0 {
				if !layoutExists(items, p.Name) {
					resp.Error = fmt.Sprintf("layout %q is not available; use Layout.List to see options", p.Name)
					break
				}
			}
			err = s.compositor.SetLayout(p.Name)

		case "Layout.Current":
			result, err = s.currentLayout()
		case "Layout.Next":
			var p struct {
				Wrap *bool `json:"wrap,omitempty"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			result, err = s.cycleLayout(+1, p.Wrap)
		case "Layout.Prev":
			var p struct {
				Wrap *bool `json:"wrap,omitempty"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			result, err = s.cycleLayout(-1, p.Wrap)

		case "Config.Get":
			var p struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.isNiri() || (s.isMango() && isCachedAppearanceKey(p.Key)) {
				result, err = s.cfgState.GetKey(p.Key)
			} else {
				result, err = s.compositor.GetConfig(p.Key)
			}
		case "Config.Set":
			var p struct {
				Key   string      `json:"key"`
				Value interface{} `json:"value"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.isNiri() || (s.isMango() && isCachedAppearanceKey(p.Key)) {
				err = s.applyGeneratedConfig(func(st *ConfigState) (ipc.ConfigUniversal, error) {
					return st.SetKey(p.Key, p.Value)
				})
			} else {
				err = s.compositor.SetConfig(p.Key, p.Value)
			}
		case "Config.Apply":
			var p struct {
				Payload string `json:"payload"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			var payload ipc.ConfigUniversal
			if err := json.Unmarshal([]byte(p.Payload), &payload); err != nil {
				resp.Error = fmt.Sprintf("invalid json payload: %v", err)
				break
			}
			// Config.Apply is invoked via JSON-RPC and does not carry a
			// [target] section, so fall back to the compositor's default
			// output paths.
			handler := NewConfigHandler(s.compositor)
			err = handler.ApplyConfig(payload)
			if err == nil {
				s.cfgState.Seed(payload)
				s.syncKeyMonitor()
			}

		case "Config.Batch":
			var p struct {
				Configs map[string]interface{} `json:"configs"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.isNiri() {
				err = s.applyGeneratedConfig(func(st *ConfigState) (ipc.ConfigUniversal, error) {
					return st.SetKeys(p.Configs)
				})
			} else if s.isMango() {
				stateConfigs := make(map[string]interface{})
				runtimeConfigs := make(map[string]interface{})
				for key, value := range p.Configs {
					if isCachedAppearanceKey(key) {
						stateConfigs[key] = value
					} else {
						runtimeConfigs[key] = value
					}
				}
				if len(stateConfigs) > 0 {
					err = s.applyGeneratedConfig(func(st *ConfigState) (ipc.ConfigUniversal, error) {
						return st.SetKeys(stateConfigs)
					})
				}
				if err == nil && len(runtimeConfigs) > 0 {
					err = s.compositor.BatchConfig(runtimeConfigs)
				}
			} else {
				err = s.compositor.BatchConfig(p.Configs)
			}
		case "Config.KeybindsBatch":
			var p struct {
				Payload string `json:"payload"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.isNiri() || s.isMango() {
				var kbPayload ipc.BatchKeybindsPayload
				if uerr := json.Unmarshal([]byte(p.Payload), &kbPayload); uerr != nil {
					resp.Error = fmt.Sprintf("invalid keybinds payload: %v", uerr)
					break
				}
				err = s.applyGeneratedConfig(func(st *ConfigState) (ipc.ConfigUniversal, error) {
					return st.ApplyKeybinds(kbPayload)
				})
			} else {
				err = s.compositor.BatchKeybinds(p.Payload)
				if serr := s.mergeKeybindsPayload(p.Payload); serr == nil {
					s.syncKeyMonitor()
				}
			}
		case "Config.RawBatch":
			var p struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.compositor.RawBatch(p.Command)
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
			if s.isMango() {
				bind := ipc.Keybind{
					Enabled: true, Modifiers: strings.Fields(p.Mods), Key: p.Key,
					Dispatcher: "exec", Argument: p.Command,
				}
				err = s.applyGeneratedConfig(func(st *ConfigState) (ipc.ConfigUniversal, error) {
					return st.ApplyKeybinds(ipc.BatchKeybindsPayload{Binds: []ipc.Keybind{bind}})
				})
				break
			}
			if ipc.ModifierSelfGroupFromParts(strings.Fields(p.Mods), p.Key) != "" {
				bind := ipc.Keybind{
					Enabled:    true,
					Modifiers:  strings.Fields(p.Mods),
					Key:        p.Key,
					Dispatcher: "exec",
					Argument:   p.Command,
				}
				if _, aerr := s.cfgState.ApplyKeybinds(ipc.BatchKeybindsPayload{
					Binds: []ipc.Keybind{bind},
				}); aerr == nil {
					s.syncKeyMonitor()
				}
				if !s.isNiri() {
					if b, merr := json.Marshal(ipc.BatchKeybindsPayload{Binds: []ipc.Keybind{bind}}); merr == nil {
						err = s.compositor.BatchKeybinds(string(b))
					}
				}
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
			if s.isMango() {
				err = s.applyGeneratedConfig(func(st *ConfigState) (ipc.ConfigUniversal, error) {
					return st.ApplyKeybinds(ipc.BatchKeybindsPayload{
						Unbinds: []ipc.KeybindTarget{{Modifiers: strings.Fields(p.Mods), Key: p.Key}},
					})
				})
				break
			}
			if ipc.ModifierSelfGroupFromParts(strings.Fields(p.Mods), p.Key) != "" {
				if _, aerr := s.cfgState.ApplyKeybinds(ipc.BatchKeybindsPayload{
					Unbinds: []ipc.KeybindTarget{{Modifiers: strings.Fields(p.Mods), Key: p.Key}},
				}); aerr == nil {
					s.syncKeyMonitor()
				}
				if !s.isNiri() {
					err = s.compositor.UnbindKey(p.Mods, p.Key)
				}
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
		case "System.GetCapabilities":
			var caps ipc.Capabilities
			caps, err = s.compositor.GetCapabilities()
			if err == nil {
				result = caps
			}
		case "System.GetCompositor":
			result = s.compositorName()
		case "System.KeymonStatus":
			result = s.keyMon.Status()
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
		case "System.IdleMonitorCreate":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			var p struct {
				TimeoutMs         *uint32 `json:"timeout_ms"`
				RespectInhibitors *bool   `json:"respect_inhibitors"`
				Enabled           *bool   `json:"enabled"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			timeoutMs := uint32(0)
			respectInhibitors := true
			enabled := true
			if p.TimeoutMs != nil {
				timeoutMs = *p.TimeoutMs
			}
			if p.RespectInhibitors != nil {
				respectInhibitors = *p.RespectInhibitors
			}
			if p.Enabled != nil {
				enabled = *p.Enabled
			}
			state, e := s.idleMgr.CreateIdleMonitor(timeoutMs, respectInhibitors, enabled)
			err = e
			if err == nil {
				result = state
			}
		case "System.IdleMonitorUpdate":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			var p struct {
				ID                uint32  `json:"id"`
				TimeoutMs         *uint32 `json:"timeout_ms"`
				RespectInhibitors *bool   `json:"respect_inhibitors"`
				Enabled           *bool   `json:"enabled"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			current, e := s.idleMgr.GetIdleMonitor(p.ID)
			if e != nil {
				err = e
				break
			}
			timeoutMs := current.TimeoutMs
			respectInhibitors := current.RespectInhibitors
			enabled := current.Enabled
			if p.TimeoutMs != nil {
				timeoutMs = *p.TimeoutMs
			}
			if p.RespectInhibitors != nil {
				respectInhibitors = *p.RespectInhibitors
			}
			if p.Enabled != nil {
				enabled = *p.Enabled
			}
			state, e := s.idleMgr.UpdateIdleMonitor(p.ID, timeoutMs, respectInhibitors, enabled)
			err = e
			if err == nil {
				result = state
			}
		case "System.IdleMonitorGet":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			var p struct {
				ID uint32 `json:"id"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			state, e := s.idleMgr.GetIdleMonitor(p.ID)
			err = e
			if err == nil {
				result = state
			}
		case "System.IdleMonitorDestroy":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			var p struct {
				ID uint32 `json:"id"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.idleMgr.DestroyIdleMonitor(p.ID)
		case "System.IdleInhibitorCreate":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			var p struct {
				Enabled *bool `json:"enabled"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			enabled := false
			if p.Enabled != nil {
				enabled = *p.Enabled
			}
			state, e := s.idleMgr.CreateIdleInhibitor(enabled)
			err = e
			if err == nil {
				result = state
			}
		case "System.IdleInhibitorSet":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			var p struct {
				ID      uint32 `json:"id"`
				Enabled bool   `json:"enabled"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			state, e := s.idleMgr.SetIdleInhibitorEnabled(p.ID, p.Enabled)
			err = e
			if err == nil {
				result = state
			}
		case "System.IdleInhibitorGet":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			var p struct {
				ID uint32 `json:"id"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			state, e := s.idleMgr.GetIdleInhibitor(p.ID)
			err = e
			if err == nil {
				result = state
			}
		case "System.IdleInhibitorDestroy":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			var p struct {
				ID uint32 `json:"id"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = s.idleMgr.DestroyIdleInhibitor(p.ID)
		case "System.InhibitSystem":
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
			err = s.idleMgr.InhibitSystem(p.On)
		case "System.IsSystemInhibited":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			if s.idleMgr.IsSystemInhibited() {
				result = "true"
			} else {
				result = "false"
			}
		case "System.AppInhibitCheck":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			var p struct {
				Patterns []string `json:"patterns"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			matches, e := s.idleMgr.AppInhibitorCheck(p.Patterns)
			err = e
			if err == nil {
				if s.isMango() {
					patterns := p.Patterns
					if len(patterns) == 0 {
						patterns = []string{"vlc", "mpv", "firefox", "chromium", "chrome", "brave", "vivaldi", "steam"}
					}
					for _, window := range s.cache.GetWindows() {
						for _, pattern := range patterns {
							if strings.Contains(strings.ToLower(window.AppID), strings.ToLower(pattern)) {
								matches[window.AppID] = true
								break
							}
						}
					}
				}
				result = matches
			}

		case "System.MediaInhibitCheck":
			if s.idleMgr == nil {
				resp.Error = "Idle management not supported on this session"
				break
			}
			mediaResult, e := s.idleMgr.MediaInhibitorCheck()
			err = e
			if err == nil {
				result = mediaResult
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
		case "Darkmode.On":
			err = SetDarkMode(true)
		case "Darkmode.Off":
			err = SetDarkMode(false)
		case "Darkmode.Toggle":
			dark, derr := IsDarkMode()
			if derr != nil {
				err = derr
				break
			}
			err = SetDarkMode(!dark)
		case "Darkmode.Status":
			dark, derr := IsDarkMode()
			if derr != nil {
				err = derr
				break
			}
			result = map[string]bool{"dark": dark}

		case "Brightness.List":
			var p struct {
				Monitor string `json:"monitor"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if p.Monitor == "" {
				result, err = ListBrightness()
			} else {
				targets, terr := resolveTargets(p.Monitor)
				if terr != nil {
					err = terr
					break
				}
				result = targets
			}
		case "Brightness.Get":
			var p struct {
				Monitor string `json:"monitor"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if p.Monitor == "" {
				resp.Error = "monitor name is required"
				break
			}
			v, gerr := GetBrightness(p.Monitor)
			if gerr != nil {
				err = gerr
				break
			}
			result = map[string]float64{"brightness": v}
		case "Brightness.Set":
			var p struct {
				Monitor string  `json:"monitor"`
				Value   float64 `json:"value"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if p.Monitor == "" {
				targets, lerr := ListBrightness()
				if lerr != nil {
					err = lerr
					break
				}
				applied := false
				for _, d := range targets {
					if applyErr := applyTo(d, p.Value); applyErr == nil {
						applied = true
						if cur, ok := readBroadcastValue(d); ok {
							s.broadcastBrightnessChange(MonitorKey(d), cur)
						}
					}
				}
				if !applied {
					resp.Error = "no monitors available"
					break
				}
				result = "ok"
				break
			}
			if setErr := SetBrightness(p.Monitor, p.Value); setErr != nil {
				err = setErr
				break
			}
			targets, _ := resolveTargets(p.Monitor)
			for _, d := range targets {
				if cur, ok := readBroadcastValue(d); ok {
					s.broadcastBrightnessChange(MonitorKey(d), cur)
				}
			}
		case "Brightness.Adjust":
			var p struct {
				Monitor string  `json:"monitor"`
				Delta   float64 `json:"delta"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if adjustErr := AdjustBrightness(p.Monitor, p.Delta); adjustErr != nil {
				err = adjustErr
				break
			}
			// Broadcast per-device with the actual post-apply value, not the
			// raw delta, so subscribers can treat value as absolute.
			targets, _ := resolveTargets(p.Monitor)
			for _, d := range targets {
				if cur, ok := readBroadcastValue(d); ok {
					s.broadcastBrightnessChange(MonitorKey(d), cur)
				}
			}
		case "Brightness.Save":
			var p struct {
				Monitor string `json:"monitor"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = SaveBrightness(p.Monitor)
		case "Brightness.Restore":
			var p struct {
				Monitor string `json:"monitor"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			err = RestoreBrightness(p.Monitor)

		case "System.Subscribe":
			startSubscription = true
			result = "subscribed"
		case "Application.List":
			if s.apps == nil {
				err = fmt.Errorf("application catalog is unavailable")
				break
			}
			result = s.apps.List()
		case "Application.Top":
			var p struct {
				Limit int `json:"limit"`
			}
			if err = json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.apps == nil {
				err = fmt.Errorf("application catalog is unavailable")
				break
			}
			result = s.apps.Top(p.Limit)
		case "Application.Get":
			var p struct {
				ID string `json:"id"`
			}
			if err = json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.apps == nil {
				err = fmt.Errorf("application catalog is unavailable")
				break
			}
			var found bool
			result, found = s.apps.Get(p.ID)
			if !found {
				err = fmt.Errorf("application %q not found", p.ID)
			}
		case "Application.Resolve":
			var p struct {
				Identity string `json:"identity"`
			}
			if err = json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.apps == nil {
				err = fmt.Errorf("application catalog is unavailable")
				break
			}
			var found bool
			result, found = s.apps.Resolve(p.Identity)
			if !found {
				err = fmt.Errorf("application %q not found", p.Identity)
			}
		case "Application.Search":
			var p struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			if err = json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.apps == nil {
				err = fmt.Errorf("application catalog is unavailable")
				break
			}
			result = s.apps.Search(p.Query, p.Limit)
		case "Application.Launch":
			var p struct {
				ID       string   `json:"id"`
				ActionID string   `json:"actionId"`
				URIs     []string `json:"uris"`
			}
			if err = json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.apps == nil {
				err = fmt.Errorf("application catalog is unavailable")
				break
			}
			err = s.apps.Launch(p.ID, p.ActionID, p.URIs)
		case "Application.RecordUsage":
			var p struct {
				ID string `json:"id"`
			}
			if err = json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.apps == nil {
				err = fmt.Errorf("application catalog is unavailable")
				break
			}
			err = s.apps.RecordUsage(p.ID)
		case "Application.Revision":
			s.mu.RLock()
			result = map[string]uint64{"revision": s.appRevision}
			s.mu.RUnlock()
		case "Icon.Resolve":
			var p struct {
				Name  string `json:"name"`
				Size  int    `json:"size"`
				Scale int    `json:"scale"`
			}
			if err = json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			result = map[string]string{"path": s.iconResolver.Resolve(p.Name, p.Size, p.Scale)}
		case "Icon.ResolveMany":
			var p struct {
				Names []string `json:"names"`
				Size  int      `json:"size"`
				Scale int      `json:"scale"`
			}
			if err = json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			resolved := make(map[string]string, len(p.Names))
			for _, name := range p.Names {
				resolved[name] = s.iconResolver.Resolve(name, p.Size, p.Scale)
			}
			result = resolved
		case "Icon.Theme":
			result = map[string]string{"name": s.iconResolver.Theme()}
		case "Service.Status":
			result = map[string]interface{}{
				"apiVersion":    1,
				"compositor":    s.compositorName(),
				"applications":  s.apps != nil,
				"icons":         s.iconResolver != nil,
				"notifications": s.notifications != nil && s.notifications.Status() == "available",
				"tray":          s.tray != nil && s.tray.Status() == "available",
			}
		case "Notification.List", "Notification.History":
			if s.notifications == nil {
				err = fmt.Errorf("notification service is unavailable")
				break
			}
			result = s.notifications.Snapshot()
		case "Notification.Active":
			if s.notifications == nil {
				err = fmt.Errorf("notification service is unavailable")
				break
			}
			result = s.notifications.Active()
		case "Notification.Status":
			if s.notifications == nil {
				result = map[string]string{"status": "unavailable"}
			} else {
				result = map[string]string{"status": s.notifications.Status()}
			}
		case "Notification.Send":
			var p struct {
				AppName         string `json:"appName"`
				AppIcon         string `json:"appIcon"`
				Summary         string `json:"summary"`
				Body            string `json:"body"`
				Image           string `json:"image"`
				Urgency         string `json:"urgency"`
				Timeout         int32  `json:"expireTimeout"`
				HistoryPriority int    `json:"historyPriority"`
				ReplaceKey      string `json:"replaceKey"`
				OwnerToken      string `json:"ownerToken"`
				Popup           *bool  `json:"popup"`
				Actions         []struct {
					ID    string `json:"identifier"`
					Label string `json:"text"`
				} `json:"actions"`
			}
			if err = json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.notifications == nil {
				err = fmt.Errorf("notification service is unavailable")
				break
			}
			actions := make([]notifications.Action, 0, len(p.Actions))
			for _, action := range p.Actions {
				actions = append(actions, notifications.Action{ID: action.ID, Label: action.Label})
			}
			urgency := uint8(1)
			switch strings.ToLower(p.Urgency) {
			case "low", "0":
				urgency = 0
			case "critical", "2":
				urgency = 2
			}
			timeout := p.Timeout
			popup := true
			if p.Popup != nil {
				popup = *p.Popup
			}
			var id uint32
			id, err = s.notifications.NotifyInternal(notifications.Notification{
				AppName: p.AppName, AppIcon: p.AppIcon, Summary: p.Summary, Body: p.Body, Image: p.Image,
				Urgency: urgency, Timeout: timeout, HistoryPriority: p.HistoryPriority, ReplaceKey: p.ReplaceKey,
				OwnerToken: p.OwnerToken, Actions: actions, Popup: popup,
			})
			if err == nil {
				result = map[string]uint32{"id": id}
			}
		case "Notification.Close", "Notification.Dismiss", "Notification.Delete":
			var p struct {
				ID uint32 `json:"id"`
			}
			if err = json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.notifications == nil {
				err = fmt.Errorf("notification service is unavailable")
				break
			}
			if req.Method == "Notification.Dismiss" {
				s.notifications.Dismiss(p.ID)
			} else if req.Method == "Notification.Delete" {
				s.notifications.Delete(p.ID)
			} else {
				s.notifications.CloseUser(p.ID)
			}
		case "Notification.Action":
			var p struct {
				ID       uint32 `json:"id"`
				ActionID string `json:"actionId"`
			}
			if err = json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.notifications == nil {
				err = fmt.Errorf("notification service is unavailable")
				break
			}
			err = s.notifications.InvokeAction(p.ID, p.ActionID)
		case "Notification.Pause", "Notification.Resume":
			var p struct {
				ID uint32 `json:"id"`
			}
			if err = json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.notifications == nil {
				err = fmt.Errorf("notification service is unavailable")
				break
			}
			if req.Method == "Notification.Pause" {
				s.notifications.Pause(p.ID)
			} else {
				s.notifications.Resume(p.ID)
			}
		case "Notification.ClearHistory":
			if s.notifications == nil {
				err = fmt.Errorf("notification service is unavailable")
				break
			}
			err = s.notifications.ClearHistory()
		case "Notification.Clear":
			if s.notifications == nil {
				err = fmt.Errorf("notification service is unavailable")
				break
			}
			err = s.notifications.ClearAll()
		case "Notification.SetDND":
			var p struct {
				Enabled bool `json:"enabled"`
			}
			if err = json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.notifications == nil {
				err = fmt.Errorf("notification service is unavailable")
				break
			}
			s.notifications.SetDoNotDisturb(p.Enabled)
		case "Notification.DND":
			if s.notifications == nil {
				result = map[string]bool{"enabled": false}
			} else {
				result = map[string]bool{"enabled": s.notifications.DoNotDisturb()}
			}
		case "Tray.List":
			if s.tray == nil {
				err = fmt.Errorf("tray service is unavailable")
				break
			}
			result = s.tray.List()
		case "Tray.Status":
			if s.tray == nil {
				result = map[string]string{"status": "unavailable"}
			} else {
				result = map[string]string{"status": s.tray.Status()}
			}
		case "Tray.Activate", "Tray.SecondaryActivate", "Tray.ContextMenu":
			var p struct {
				ID string `json:"id"`
				X  int32  `json:"x"`
				Y  int32  `json:"y"`
			}
			if err = json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.tray == nil {
				err = fmt.Errorf("tray service is unavailable")
				break
			}
			switch req.Method {
			case "Tray.Activate":
				err = s.tray.Activate(p.ID, p.X, p.Y)
			case "Tray.SecondaryActivate":
				err = s.tray.SecondaryActivate(p.ID, p.X, p.Y)
			default:
				err = s.tray.ContextMenu(p.ID, p.X, p.Y)
			}
		case "Tray.Scroll":
			var p struct {
				ID          string `json:"id"`
				Delta       int32  `json:"delta"`
				Orientation string `json:"orientation"`
			}
			if err = json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.tray == nil {
				err = fmt.Errorf("tray service is unavailable")
				break
			}
			err = s.tray.Scroll(p.ID, p.Delta, p.Orientation)
		case "Tray.Menu":
			var p struct {
				ID string `json:"id"`
			}
			if err = json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.tray == nil {
				err = fmt.Errorf("tray service is unavailable")
				break
			}
			var revision uint32
			var items []tray.MenuItem
			revision, items, err = s.tray.Menu(p.ID)
			if err == nil {
				result = map[string]interface{}{"revision": revision, "items": items}
			}
		case "Tray.MenuEvent":
			var p struct {
				ID     string `json:"id"`
				MenuID int32  `json:"menuId"`
				Event  string `json:"event"`
			}
			if err = json.Unmarshal(req.Params, &p); err != nil {
				resp.Error = fmt.Sprintf("invalid params: %v", err)
				break
			}
			if s.tray == nil {
				err = fmt.Errorf("tray service is unavailable")
				break
			}
			if p.Event == "" {
				p.Event = "clicked"
			}
			err = s.tray.MenuEvent(p.ID, p.MenuID, p.Event)

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

		if err := enc.Encode(resp); err != nil {
			return
		}
		if startSubscription {
			s.startSubscription(conn)
			for {
				var ignored json.RawMessage
				if err := dec.Decode(&ignored); err != nil {
					return
				}
			}
		}
	}
}

func (s *Server) startSubscription(conn net.Conn) {
	s.clientsMu.Lock()
	if ec, ok := s.clients[conn]; ok {
		close(ec.out)
	}
	ec := &eventClient{conn: conn, out: make(chan []byte, eventClientQueueSize)}
	s.clients[conn] = ec
	s.clientsMu.Unlock()
	go ec.writeLoop()
	notif := Notification{
		JSONRPC: "2.0",
		Method:  "State.Dump",
		State: &StateDump{
			Windows:    s.cache.GetWindows(),
			Workspaces: s.cache.GetWorkspaces(),
			Monitors:   s.cache.GetMonitors(),
		},
	}
	if data, err := json.Marshal(notif); err == nil {
		data = append(data, '\n')
		ec.out <- data
	}
}

type StateDump struct {
	Windows      []ipc.Window    `json:"windows"`
	Workspaces   []ipc.Workspace `json:"workspaces"`
	Monitors     []ipc.Monitor   `json:"monitors"`
	OverviewOpen *bool           `json:"overview_open,omitempty"`
}

type Notification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	State   *StateDump  `json:"state,omitempty"`
}

func (s *Server) getOverviewOpen() *bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.overviewOpen
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
		State: &StateDump{
			Windows:      s.cache.GetWindows(),
			Workspaces:   s.cache.GetWorkspaces(),
			Monitors:     s.cache.GetMonitors(),
			OverviewOpen: s.getOverviewOpen(),
		},
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return
	}
	data = append(data, '\n')

	for _, ec := range s.clients {
		select {
		case ec.out <- data:
		default:
			_ = ec.conn.Close()
		}
	}
}

func (s *Server) broadcastStandaloneEvent(method string, params interface{}) {
	data, err := json.Marshal(Notification{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return
	}
	data = append(data, '\n')
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	for _, ec := range s.clients {
		select {
		case ec.out <- data:
		default:
			_ = ec.conn.Close()
		}
	}
}

func (s *Server) handleIdleMonitorChanged(id uint32, isIdle bool) {
	params := map[string]interface{}{
		"id":      id,
		"is_idle": isIdle,
	}
	s.broadcastEvent("Event.IdleMonitorChanged", params)
}

func (s *Server) broadcastBrightnessChange(monitor string, value float64) {
	params := map[string]interface{}{
		"monitor": monitor,
		"value":   value,
	}
	s.broadcastEvent("Event.BrightnessChanged", params)
}
