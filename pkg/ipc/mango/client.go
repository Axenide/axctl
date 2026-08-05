package mango

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"axctl/pkg/ipc"
)

type Mango struct {
	mu         sync.Mutex
	conn       *mmsgConn
	socketPath string

	eventCh    chan ipc.Event
	subscribed bool
	watch      *watchSession
}

func New() (*Mango, error) {
	return NewWithSocket("")
}

func NewWithSocket(socketPath string) (*Mango, error) {
	if socketPath == "" {
		p, err := resolveSocketPath()
		if err != nil {
			return nil, err
		}
		socketPath = p
	}
	conn, err := dialMango(socketPath, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("mango: %w", err)
	}
	var probe struct{}
	if err := conn.Query("get cursorpos", &probe); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("mango: validate connection: %w", err)
	}
	return &Mango{conn: conn, socketPath: socketPath}, nil
}

func (m *Mango) SocketPath() string { return m.socketPath }

func (m *Mango) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.watch != nil {
		m.watch.Close()
		m.watch = nil
	}
	if m.conn != nil {
		err := m.conn.Close()
		m.conn = nil
		return err
	}
	return nil
}

func (m *Mango) acquire() (*mmsgConn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn == nil {
		return nil, fmt.Errorf("mango: connection closed")
	}
	return m.conn, nil
}

func parseClientID(id string) (uint64, error) {
	if id == "" {
		return 0, fmt.Errorf("empty client id")
	}
	v, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid client id %q: %w", id, err)
	}
	return v, nil
}

func clientTarget(id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("client id required")
	}
	if _, err := parseClientID(id); err != nil {
		return "", err
	}
	return "client," + id, nil
}

func (m *Mango) queryClientState(id string) (*mangoClient, error) {
	conn, err := m.acquire()
	if err != nil {
		return nil, err
	}
	var c mangoClient
	if err := conn.Query("get client "+id, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (m *Mango) ListWindows() ([]ipc.Window, error) {
	conn, err := m.acquire()
	if err != nil {
		return nil, err
	}
	var raw []mangoClient
	if err := conn.Query("get all-clients", &raw); err != nil {
		return nil, err
	}
	wins := make([]ipc.Window, 0, len(raw))
	for i := range raw {
		c := &raw[i]
		var wsID string
		for _, t := range c.Tags {
			if t != 0 {
				wsID = strconv.Itoa(t)
				break
			}
		}
		wins = append(wins, ipc.Window{
			ID:           strconv.FormatUint(c.ID, 10),
			Title:        c.Title,
			AppID:        c.AppID,
			WorkspaceID:  wsID,
			IsFloating:   c.Floating != 0,
			IsFullscreen: c.Fullscreen != 0,
			Metadata: map[string]interface{}{
				"monitor":    c.MonitorName,
				"monitor_id": c.MonitorName,
				"x":          c.X,
				"y":          c.Y,
				"width":      c.Width,
				"height":     c.Height,
				"maximized":  c.Maximized != 0,
				"global":     c.Global != 0,
			},
		})
	}
	return wins, nil
}

func (m *Mango) ActiveWindow() (string, error) {
	conn, err := m.acquire()
	if err != nil {
		return "", err
	}
	var cur mangoClient
	if err := conn.Query("get focusing-client", &cur); err != nil {
		return "", err
	}
	if cur.ID == 0 {
		return "", nil
	}
	return strconv.FormatUint(cur.ID, 10), nil
}

func (m *Mango) FocusWindow(id string) error {
	target, err := clientTarget(id)
	if err != nil {
		return err
	}
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch("focusid " + target)
}

func (m *Mango) FocusDir(direction string) error {
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch("focusdir " + normalizeDirection(direction))
}

func (m *Mango) CloseWindow(id string) error {
	target, err := clientTarget(id)
	if err != nil {
		return err
	}
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch("killclient " + target)
}

func (m *Mango) MoveWindow(id string, direction string) error {
	target, err := clientTarget(id)
	if err != nil {
		return err
	}
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch(fmt.Sprintf("smartmovewin %s %s", target, normalizeDirection(direction)))
}

func (m *Mango) ResizeWindow(id string, width, height int) error {
	target, err := clientTarget(id)
	if err != nil {
		return err
	}
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch(fmt.Sprintf("resizewin %s %d %d", target, width, height))
}

func (m *Mango) ToggleFloating(id string) error {
	target, err := clientTarget(id)
	if err != nil {
		return err
	}
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch("togglefloating " + target)
}

func (m *Mango) SetFullscreen(id string, state bool) error {
	if _, err := parseClientID(id); err != nil {
		return err
	}
	c, err := m.queryClientState(id)
	if err != nil {
		return err
	}
	current := c.Fullscreen != 0
	if current == state {
		return nil
	}
	target, err := clientTarget(id)
	if err != nil {
		return err
	}
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch("togglefullscreen " + target)
}

func (m *Mango) SetMaximized(id string, state bool) error {
	if _, err := parseClientID(id); err != nil {
		return err
	}
	c, err := m.queryClientState(id)
	if err != nil {
		return err
	}
	current := c.Maximized != 0
	if current == state {
		return nil
	}
	target, err := clientTarget(id)
	if err != nil {
		return err
	}
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch("togglemaximizescreen " + target)
}

func (m *Mango) PinWindow(id string, state bool) error {
	if _, err := parseClientID(id); err != nil {
		return err
	}
	c, err := m.queryClientState(id)
	if err != nil {
		return err
	}
	current := c.Global != 0
	if current == state {
		return nil
	}
	target, err := clientTarget(id)
	if err != nil {
		return err
	}
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch("toggleglobal " + target)
}

func (m *Mango) ToggleGroup(id string) error     { return ipc.ErrNotSupported }
func (m *Mango) GroupNav(direction string) error { return ipc.ErrNotSupported }
func (m *Mango) SetLayoutProperty(id string, key, value string) error {
	return ipc.ErrNotSupported
}

func (m *Mango) MoveWindowPixel(id string, x, y int) error {
	target, err := clientTarget(id)
	if err != nil {
		return err
	}
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch(fmt.Sprintf("movewin %s %d %d", target, x, y))
}

func (m *Mango) ListWorkspaces() ([]ipc.Workspace, error) {
	conn, err := m.acquire()
	if err != nil {
		return nil, err
	}
	var tags []mangoTag
	if err := conn.Query("get all-tags", &tags); err != nil {
		return nil, err
	}
	out := make([]ipc.Workspace, 0, len(tags))
	for i := range tags {
		t := &tags[i]
		out = append(out, ipc.Workspace{
			ID:       t.Name,
			Name:     t.Name,
			IsActive: t.Active != 0,
			Metadata: map[string]interface{}{
				"index":   t.Index,
				"urgent":  t.Urgent != 0,
				"focused": t.Active != 0,
			},
		})
	}
	return out, nil
}

func (m *Mango) ActiveWorkspace() (*ipc.Workspace, error) {
	conn, err := m.acquire()
	if err != nil {
		return nil, err
	}
	var tags []mangoTag
	if err := conn.Query("get all-tags", &tags); err != nil {
		return nil, err
	}
	for i := range tags {
		t := &tags[i]
		if t.Active != 0 {
			return &ipc.Workspace{
				ID:       t.Name,
				Name:     t.Name,
				IsActive: true,
				Metadata: map[string]interface{}{
					"index":   t.Index,
					"focused": true,
				},
			}, nil
		}
	}
	return nil, fmt.Errorf("no active tag")
}

func (m *Mango) SwitchWorkspace(id string) error {
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch("view " + id)
}

func (m *Mango) MoveToWorkspace(windowID, workspaceID string) error {
	target, err := clientTarget(windowID)
	if err != nil {
		return err
	}
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch("tag " + target + " " + workspaceID)
}

func (m *Mango) MoveToWorkspaceSilent(windowID, workspaceID string) error {
	target, err := clientTarget(windowID)
	if err != nil {
		return err
	}
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch("tagsilent " + target + " " + workspaceID)
}

func (m *Mango) ToggleSpecialWorkspace(name string) error {
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	if name == "" {
		return conn.Dispatch("toggle_scratchpad")
	}
	return conn.Dispatch("toggle_named_scratchpad " + name)
}

func (m *Mango) ListMonitors() ([]ipc.Monitor, error) {
	conn, err := m.acquire()
	if err != nil {
		return nil, err
	}
	var ms []mangoMonitor
	if err := conn.Query("get all-monitors", &ms); err != nil {
		return nil, err
	}
	out := make([]ipc.Monitor, 0, len(ms))
	for i := range ms {
		mm := &ms[i]
		scale := 1.0
		if mm.Scale > 0 {
			scale = float64(mm.Scale) / 100.0
		}
		out = append(out, ipc.Monitor{
			ID:        mm.Name,
			Name:      mm.Name,
			Width:     mm.Width,
			Height:    mm.Height,
			Scale:     scale,
			IsFocused: mm.Focused != 0,
			Metadata: map[string]interface{}{
				"x":      mm.X,
				"y":      mm.Y,
				"index":  mm.Index,
				"active": mm.Active != 0,
			},
		})
	}
	return out, nil
}

func (m *Mango) FocusMonitor(id string) error {
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch("focusmon " + id)
}

func (m *Mango) MoveToMonitor(windowID, monitorID string) error {
	target, err := clientTarget(windowID)
	if err != nil {
		return err
	}
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch("tagmon " + target + " " + monitorID)
}

func (m *Mango) SetDpms(monitorID string, on bool) error {
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	cmd := "wakeup_monitor"
	if !on {
		cmd = "sleep_monitor"
	}
	return conn.Dispatch(cmd + " " + monitorID)
}

func (m *Mango) SetLayout(name string) error {
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch("setlayout " + name)
}

func (m *Mango) GetConfig(key string) (interface{}, error) {
	return nil, ipc.ErrNotSupported
}

func (m *Mango) SetConfig(key string, value interface{}) error {
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	v := fmt.Sprint(value)
	switch key {
	case "gaps.inner":
		if err := conn.Dispatch("setoption gappih " + v); err != nil {
			return err
		}
		return conn.Dispatch("setoption gappiv " + v)
	case "gaps.outer":
		if err := conn.Dispatch("setoption gappoh " + v); err != nil {
			return err
		}
		return conn.Dispatch("setoption gappov " + v)
	case "border.width":
		return conn.Dispatch("setoption borderpx " + v)
	case "border.active_color":
		return conn.Dispatch("setoption focuscolor " + ipc.MangoColor(v))
	case "border.inactive_color":
		return conn.Dispatch("setoption bordercolor " + ipc.MangoColor(v))
	case "opacity.active":
		return conn.Dispatch("setoption focused_opacity " + v)
	case "opacity.inactive":
		return conn.Dispatch("setoption unfocused_opacity " + v)
	case "blur.enabled":
		return conn.Dispatch("setoption blur " + v)
	case "blur.size":
		return conn.Dispatch("setoption blur_params_radius " + v)
	case "blur.passes":
		return conn.Dispatch("setoption blur_params_num_passes " + v)
	case "blur.brightness":
		return conn.Dispatch("setoption blur_params_brightness " + v)
	case "blur.contrast":
		return conn.Dispatch("setoption blur_params_contrast " + v)
	case "blur.saturation":
		return conn.Dispatch("setoption blur_params_saturation " + v)
	case "shadows":
		return conn.Dispatch("setoption shadows " + v)
	case "rounding", "border_radius":
		return conn.Dispatch("setoption border_radius " + v)
	default:
		return ipc.ErrNotSupported
	}
}

func (m *Mango) BatchConfig(configs map[string]interface{}) error {
	for k, v := range configs {
		if err := m.SetConfig(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (m *Mango) BatchKeybinds(jsonPayload string) error { return ipc.ErrNotSupported }
func (m *Mango) RawBatch(command string) error          { return ipc.ErrNotSupported }

func (m *Mango) ReloadConfig() error {
	return m.LoadConfig("")
}

func (m *Mango) LoadConfig(path string) error {
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	if path == "" {
		return conn.Dispatch("reload_config")
	}
	return conn.Dispatch("load_config_file " + path)
}

func (m *Mango) GetAnimations() (interface{}, error) { return nil, ipc.ErrNotSupported }

func (m *Mango) GetCursorPosition() (int, int, error) {
	conn, err := m.acquire()
	if err != nil {
		return 0, 0, err
	}
	var cur mangoCursorPos
	if err := conn.Query("get cursorpos", &cur); err != nil {
		return 0, 0, err
	}
	return cur.X, cur.Y, nil
}

func (m *Mango) BindKey(mods, key, command string) error { return ipc.ErrNotSupported }
func (m *Mango) UnbindKey(mods, key string) error        { return ipc.ErrNotSupported }

func (m *Mango) Execute(command string) error {
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch("spawn_shell " + command)
}

func (m *Mango) Exit() error {
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch("quit")
}

func (m *Mango) SwitchKeyboardLayout(action string) error {
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch("switch_keyboard_layout " + action)
}

func (m *Mango) SetKeyboardLayouts(layouts string, variants string) error {
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	if variants != "" {
		if err := conn.Dispatch("setoption xkb_rules_variant " + variants); err != nil {
			return err
		}
		return conn.Dispatch("setoption xkb_rules_layout " + layouts)
	}
	if err := conn.Dispatch("setoption xkb_rules_variant  "); err != nil {
		return err
	}
	return conn.Dispatch("setoption xkb_rules_layout " + layouts)
}

func (m *Mango) Subscribe() (<-chan ipc.Event, error) {
	m.mu.Lock()
	if m.subscribed {
		m.mu.Unlock()
		ch := m.eventCh
		if ch == nil {
			return nil, fmt.Errorf("already subscribed")
		}
		return ch, nil
	}
	ch := make(chan ipc.Event, 64)
	m.eventCh = ch
	m.subscribed = true
	socketPath := m.socketPath
	m.mu.Unlock()

	events := []string{"all-clients", "all-monitors", "all-tags", "focusing-client"}
	ws, err := openWatches(socketPath, events)
	if err != nil {
		m.mu.Lock()
		m.subscribed = false
		m.mu.Unlock()
		close(ch)
		return nil, fmt.Errorf("subscribe: %w", err)
	}

	m.mu.Lock()
	m.watch = ws
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			m.subscribed = false
			m.watch = nil
			m.mu.Unlock()
			close(ch)
		}()
		for ev := range ws.Updates() {
			if ev.Type == "" {
				continue
			}
			select {
			case ch <- ev:
			default:
			}
		}
	}()

	return ch, nil
}

func (m *Mango) GetCapabilities() (ipc.Capabilities, error) {
	return ipc.Capabilities{
		WindowsSupported:    true,
		WorkspacesSupported: true,
	}, nil
}

func normalizeDirection(dir string) string {
	switch dir {
	case "l":
		return "left"
	case "r":
		return "right"
	case "u":
		return "up"
	case "d":
		return "down"
	default:
		return dir
	}
}
