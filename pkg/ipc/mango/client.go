package mango

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"axctl/pkg/ipc"
)

type Mango struct {
	mu         sync.Mutex
	conn       *mmsgConn
	socketPath string
	closed     bool

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
		return nil, fmt.Errorf("mango: validate connection: %w", err)
	}
	return &Mango{socketPath: socketPath}, nil
}

func (m *Mango) SocketPath() string { return m.socketPath }

func (m *Mango) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
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
	if m.closed {
		m.mu.Unlock()
		return nil, fmt.Errorf("mango: connection closed")
	}
	socketPath := m.socketPath
	m.mu.Unlock()
	if socketPath == "" {
		return nil, fmt.Errorf("mango: socket path is empty")
	}
	return dialMango(socketPath, 3*time.Second)
}

func (m *Mango) query(command string, out interface{}) error {
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Query(command, out)
}

func (m *Mango) dispatch(function string, args []string, clientID string) error {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, function)
	parts = append(parts, args...)
	payload := strings.Join(parts, ",")
	if clientID != "" {
		target, err := clientTarget(clientID)
		if err != nil {
			return err
		}
		payload += " " + target
	}
	conn, err := m.acquire()
	if err != nil {
		return err
	}
	return conn.Dispatch(payload)
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
	if _, err := parseClientID(id); err != nil {
		return nil, err
	}
	var c mangoClient
	if err := m.query("get client "+id, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (m *Mango) ListWindows() ([]ipc.Window, error) {
	var response mangoClients
	if err := m.query("get all-clients", &response); err != nil {
		return nil, err
	}
	wins := make([]ipc.Window, 0, len(response.Clients))
	for i := range response.Clients {
		wins = append(wins, windowFromMango(&response.Clients[i]))
	}
	return wins, nil
}

func windowFromMango(c *mangoClient) ipc.Window {
	wsID := ""
	if c.Monitor != "" && len(c.Tags) > 0 {
		wsID = workspaceID(c.Monitor, primaryTag(c.Tags))
	}
	tags := append([]int(nil), c.Tags...)
	return ipc.Window{
		ID:           strconv.FormatUint(c.ID, 10),
		Title:        c.Title,
		AppID:        c.AppID,
		WorkspaceID:  wsID,
		IsFocused:    c.Focused.value(),
		IsFloating:   c.Floating.value(),
		IsFullscreen: c.Fullscreen.value(),
		IsHidden:     c.Minimized.value() || c.Scratchpad.value() || c.NamedScratchpad.value(),
		Metadata: map[string]interface{}{
			"monitor":             c.Monitor,
			"monitor_id":          c.Monitor,
			"is_visible":          c.Visible.value(),
			"pid":                 c.PID,
			"foreign_toplevel_id": c.ForeignID,
			"tags":                tags,
			"maximized":           c.Maximized.value(),
			"global":              c.Global.value(),
			"overlay":             c.Overlay.value(),
			"fake_fullscreen":     c.FakeFullscreen.value(),
			"urgent":              c.Urgent.value(),
			"is_group":            c.IsGroup.value(),
			"is_group_focus":      c.GroupFocus.value(),
			"scratchpad":          c.Scratchpad.value(),
			"named_scratchpad":    c.NamedScratchpad.value(),
			"xwayland":            c.XWayland.value(),
			"x":                   c.X,
			"y":                   c.Y,
			"width":               c.Width,
			"height":              c.Height,
			"scroller_proportion": c.ScrollerRatio,
		},
	}
}

func primaryTag(tags []int) int {
	for _, tag := range tags {
		if tag == 0 {
			return 0
		}
	}
	for _, tag := range tags {
		if tag > 0 {
			return tag
		}
	}
	return 0
}

func workspaceID(monitor string, tag int) string {
	return monitor + ":" + strconv.Itoa(tag)
}

func mangoOverviewOpen(activeTags []int) bool {
	for _, tag := range activeTags {
		if tag == 0 {
			return true
		}
	}
	return false
}

func (m *Mango) ActiveWindow() (string, error) {
	var cur mangoClient
	if err := m.query("get focusing-client", &cur); err != nil {
		if strings.Contains(err.Error(), "no focused client") {
			return "", nil
		}
		return "", err
	}
	if cur.ID == 0 {
		return "", nil
	}
	return strconv.FormatUint(cur.ID, 10), nil
}

func (m *Mango) FocusWindow(id string) error {
	return m.dispatch("focusid", nil, id)
}

func (m *Mango) FocusDir(direction string) error {
	dir, err := mangoDirection(direction)
	if err != nil {
		return err
	}
	return m.dispatch("focusdir", []string{dir}, "")
}

func (m *Mango) CloseWindow(id string) error {
	return m.dispatch("killclient", nil, id)
}

func (m *Mango) MoveWindow(id string, direction string) error {
	dir, err := mangoDirection(direction)
	if err != nil {
		return err
	}
	c, err := m.queryClientState(id)
	if err != nil {
		return err
	}
	function := "move_client"
	if c.Floating.value() {
		function = "smartmovewin"
	}
	return m.dispatch(function, []string{dir}, id)
}

func (m *Mango) ResizeWindow(id string, width, height int) error {
	c, err := m.queryClientState(id)
	if err != nil {
		return err
	}
	if width < 0 || height < 0 {
		return fmt.Errorf("window dimensions must not be negative")
	}
	if !c.Floating.value() {
		width -= c.Width
		height -= c.Height
	}
	return m.dispatch("resizewin", []string{strconv.Itoa(width), strconv.Itoa(height)}, id)
}

func (m *Mango) ToggleFloating(id string) error {
	return m.dispatch("togglefloating", nil, id)
}

func (m *Mango) SetFullscreen(id string, state bool) error {
	if _, err := parseClientID(id); err != nil {
		return err
	}
	c, err := m.queryClientState(id)
	if err != nil {
		return err
	}
	current := c.Fullscreen.value()
	if current == state {
		return nil
	}
	return m.dispatch("togglefullscreen", nil, id)
}

func (m *Mango) SetMaximized(id string, state bool) error {
	if _, err := parseClientID(id); err != nil {
		return err
	}
	c, err := m.queryClientState(id)
	if err != nil {
		return err
	}
	current := c.Maximized.value()
	if current == state {
		return nil
	}
	return m.dispatch("togglemaximizescreen", nil, id)
}

func (m *Mango) SetFakeFullscreen(id string, state bool) error {
	c, err := m.queryClientState(id)
	if err != nil {
		return err
	}
	if c.FakeFullscreen.value() == state {
		return nil
	}
	return m.dispatch("togglefakefullscreen", nil, id)
}

func (m *Mango) PinWindow(id string, state bool) error {
	if _, err := parseClientID(id); err != nil {
		return err
	}
	c, err := m.queryClientState(id)
	if err != nil {
		return err
	}
	current := c.Global.value()
	if current == state {
		return nil
	}
	return m.dispatch("toggleglobal", nil, id)
}

func (m *Mango) ToggleGroup(id string) error { return ipc.ErrNotSupported }

func (m *Mango) GroupNav(direction string) error {
	dir := ""
	switch direction {
	case "", "f", "r", "next", "forward":
		dir = "next"
	case "l", "u", "b", "prev", "backward":
		dir = "prev"
	default:
		return fmt.Errorf("invalid group direction %q", direction)
	}
	return m.dispatch("groupfocus", []string{dir}, "")
}

func (m *Mango) SetLayoutProperty(id string, key, value string) error {
	if key != "proportion" && key != "scroller_proportion" {
		return ipc.ErrNotSupported
	}
	layouts, err := m.ListLayouts()
	if err != nil {
		return err
	}
	currentLayout := ""
	for _, layout := range layouts {
		if layout.Current {
			currentLayout = layout.Name
			break
		}
	}
	if currentLayout != "scroller" && currentLayout != "vertical_scroller" {
		return ipc.ErrNotSupported
	}
	clientID := id
	if clientID == "" {
		clientID, err = m.ActiveWindow()
		if err != nil {
			return err
		}
		if clientID == "" {
			return fmt.Errorf("no active window for scroller proportion")
		}
	}
	client, err := m.queryClientState(clientID)
	if err != nil {
		return err
	}
	monitor, err := m.selectedMonitor()
	if err != nil {
		return err
	}
	if client.Monitor != monitor {
		return ipc.ErrNotSupported
	}
	value = strings.TrimSpace(value)
	proportion, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fmt.Errorf("invalid scroller proportion %q: %w", value, err)
	}
	if math.IsNaN(proportion) || math.IsInf(proportion, 0) {
		return fmt.Errorf("invalid scroller proportion %q", value)
	}
	if strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		if client.ScrollerRatio < 0.1 || client.ScrollerRatio > 1 {
			return fmt.Errorf("window %s has no valid scroller proportion", clientID)
		}
		proportion = math.Max(0.1, math.Min(1, client.ScrollerRatio+proportion))
	} else if proportion < 0.1 || proportion > 1 {
		return fmt.Errorf("scroller proportion %q must be between 0.1 and 1", value)
	}
	proportion = math.Round(proportion*10000) / 10000
	value = strconv.FormatFloat(proportion, 'f', -1, 64)
	return m.dispatch("set_proportion", []string{value}, clientID)
}

func (m *Mango) MoveWindowPixel(id string, x, y int) error {
	return m.dispatch("movewin", []string{strconv.Itoa(x), strconv.Itoa(y)}, id)
}

func (m *Mango) ListWorkspaces() ([]ipc.Workspace, error) {
	var response mangoAllTags
	if err := m.query("get all-tags", &response); err != nil {
		return nil, err
	}
	var layoutResponse mangoLayouts
	layoutNames := make(map[string]string)
	if err := m.query("get layouts", &layoutResponse); err == nil {
		for _, layout := range layoutResponse.Layouts {
			layoutNames[layout.Symbol] = layout.Name
		}
	}
	var monitors mangoMonitors
	if err := m.query("get all-monitors", &monitors); err != nil {
		return nil, err
	}
	var clientResponse mangoClients
	if err := m.query("get all-clients", &clientResponse); err != nil {
		return nil, err
	}
	activeTags := make(map[string]map[int]bool, len(monitors.Monitors))
	overviewOpen := make(map[string]bool, len(monitors.Monitors))
	for _, monitor := range monitors.Monitors {
		activeTags[monitor.Name] = make(map[int]bool, len(monitor.ActiveTags))
		overviewOpen[monitor.Name] = mangoOverviewOpen(monitor.ActiveTags)
		for _, tag := range monitor.ActiveTags {
			if tag > 0 {
				activeTags[monitor.Name][tag] = true
			}
		}
	}
	specialCounts := make(map[string]int)
	for i := range clientResponse.Clients {
		client := &clientResponse.Clients[i]
		for _, tag := range client.Tags {
			if tag == 0 {
				specialCounts[client.Monitor]++
				break
			}
		}
	}
	out := make([]ipc.Workspace, 0)
	for _, group := range response.Monitors {
		active := activeTags[group.Monitor]
		isOverviewOpen := overviewOpen[group.Monitor]
		for i := range group.Tags {
			t := &group.Tags[i]
			isActive := t.Active.value()
			if len(active) > 0 && !isOverviewOpen {
				isActive = active[t.Index]
			}
			name := strconv.Itoa(t.Index)
			layoutName := t.Layout
			if normalized, ok := layoutNames[t.Layout]; ok {
				layoutName = normalized
			}
			out = append(out, ipc.Workspace{
				ID:        workspaceID(group.Monitor, t.Index),
				Name:      name,
				MonitorID: group.Monitor,
				IsActive:  isActive,
				IsEmpty:   t.ClientCount == 0,
				Metadata: map[string]interface{}{
					"index":         t.Index,
					"urgent":        t.Urgent.value(),
					"focused":       isActive,
					"layout":        layoutName,
					"layout_symbol": t.Layout,
				},
			})
		}
		out = append(out, ipc.Workspace{
			ID:        workspaceID(group.Monitor, 0),
			Name:      "0",
			MonitorID: group.Monitor,
			IsActive:  false,
			IsEmpty:   true,
			Metadata: map[string]interface{}{
				"index":   0,
				"special": true,
				"focused": false,
			},
		})
		out[len(out)-1].IsEmpty = specialCounts[group.Monitor] == 0
	}
	return out, nil
}

func (m *Mango) ActiveWorkspace() (*ipc.Workspace, error) {
	monitor, err := m.selectedMonitor()
	if err != nil {
		return nil, err
	}
	workspaces, err := m.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	for i := range workspaces {
		if workspaces[i].MonitorID == monitor && workspaces[i].IsActive {
			return &workspaces[i], nil
		}
	}
	return nil, fmt.Errorf("no active workspace on monitor %q", monitor)
}

func (m *Mango) SwitchWorkspace(id string) error {
	switch id {
	case "+1", "r+1":
		return m.dispatch("viewtoright", nil, "")
	case "-1", "r-1":
		return m.dispatch("viewtoleft", nil, "")
	}
	monitor, tag, err := m.resolveWorkspace(id)
	if err != nil {
		return err
	}
	active, err := m.selectedMonitor()
	if err != nil {
		return err
	}
	if monitor != active {
		return m.dispatch("viewcrossmon", []string{strconv.Itoa(tag), monitor}, "")
	}
	return m.dispatch("view", []string{strconv.Itoa(tag)}, "")
}

func (m *Mango) MoveToWorkspace(windowID, workspaceID string) error {
	monitor, tag, err := m.resolveWorkspace(workspaceID)
	if err != nil {
		return err
	}
	return m.dispatch("tagcrossmon", []string{strconv.Itoa(tag), monitor}, windowID)
}

func (m *Mango) MoveToWorkspaceSilent(windowID, workspaceID string) error {
	monitor, tag, err := m.resolveWorkspace(workspaceID)
	if err != nil {
		return err
	}
	active, err := m.selectedMonitor()
	if err != nil {
		return err
	}
	if monitor != active {
		return ipc.ErrNotSupported
	}
	return m.dispatch("tagsilent", []string{strconv.Itoa(tag)}, windowID)
}

func (m *Mango) ToggleSpecialWorkspace(name string) error {
	if name != "" {
		return ipc.ErrNotSupported
	}
	return m.dispatch("toggle_special_tag", nil, "")
}

func (m *Mango) ToggleOverview() error {
	return m.dispatch("toggleoverview", nil, "")
}

func (m *Mango) selectedMonitor() (string, error) {
	var response mangoMonitors
	if err := m.query("get all-monitors", &response); err != nil {
		return "", err
	}
	for _, monitor := range response.Monitors {
		if monitor.Active.value() {
			return monitor.Name, nil
		}
	}
	if len(response.Monitors) > 0 {
		return response.Monitors[0].Name, nil
	}
	return "", fmt.Errorf("no active monitors")
}

func (m *Mango) resolveWorkspace(id string) (string, int, error) {
	if strings.Contains(id, ":") {
		monitor, rawTag, ok := strings.Cut(id, ":")
		if !ok || monitor == "" {
			return "", 0, fmt.Errorf("invalid workspace id %q", id)
		}
		tag, err := parseTag(rawTag)
		return monitor, tag, err
	}
	tag, err := parseTag(id)
	if err != nil {
		return "", 0, err
	}
	monitor, err := m.selectedMonitor()
	return monitor, tag, err
}

func parseTag(raw string) (int, error) {
	tag, err := strconv.Atoi(raw)
	if err != nil || tag < 0 || tag > 31 {
		return 0, fmt.Errorf("invalid Mango tag %q", raw)
	}
	return tag, nil
}

func (m *Mango) ListMonitors() ([]ipc.Monitor, error) {
	var response mangoMonitors
	if err := m.query("get all-monitors", &response); err != nil {
		return nil, err
	}
	out := make([]ipc.Monitor, 0, len(response.Monitors))
	for i := range response.Monitors {
		mm := &response.Monitors[i]
		scale := 1.0
		if mm.Scale > 0 {
			scale = mm.Scale
		}
		isOverviewOpen := mangoOverviewOpen(mm.ActiveTags)
		out = append(out, ipc.Monitor{
			ID:            mm.Name,
			Name:          mm.Name,
			Width:         mm.Width,
			Height:        mm.Height,
			LogicalWidth:  mm.Width,
			LogicalHeight: mm.Height,
			Scale:         scale,
			IsFocused:     mm.Active.value(),
			Metadata: map[string]interface{}{
				"x":             mm.X,
				"y":             mm.Y,
				"index":         mm.Index,
				"active":        mm.Active.value(),
				"layout_index":  mm.LayoutIndex,
				"layout_symbol": mm.LayoutSymbol,
				"overview_open": isOverviewOpen,
				"active_tags":   append([]int(nil), mm.ActiveTags...),
				"is_hdr":        mm.IsHDR.value(),
				"is_vrr":        mm.IsVRR.value(),
			},
		})
	}
	return out, nil
}

func (m *Mango) FocusMonitor(id string) error {
	return m.dispatch("focusmon", []string{id}, "")
}

func (m *Mango) MoveToMonitor(windowID, monitorID string) error {
	return m.dispatch("tagmon", []string{monitorID}, windowID)
}

func (m *Mango) SetDpms(monitorID string, on bool) error {
	cmd := "wakeup_monitor"
	if !on {
		cmd = "sleep_monitor"
	}
	return m.dispatch(cmd, []string{monitorID}, "")
}

func (m *Mango) SetLayout(name string) error {
	if name == "" || strings.ContainsAny(name, ", \t\r\n") {
		return fmt.Errorf("invalid Mango layout name %q", name)
	}
	layouts, err := m.ListLayouts()
	if err != nil {
		return err
	}
	if len(layouts) > 0 {
		available := false
		for _, layout := range layouts {
			if layout.Name == name {
				available = true
				break
			}
		}
		if !available {
			return fmt.Errorf("Mango layout %q is not available", name)
		}
	}
	return m.dispatch("setlayout", []string{name}, "")
}

// mangoKnownLayouts lists built-in Mango layouts recognized by setlayout /
// switch_layout (src/layout/layout.h). Used as fallback when the live list
// cannot be obtained through mmsg.
var mangoKnownLayouts = []string{
	"tile",
	"scroller",
	"grid",
	"monocle",
	"deck",
	"center_tile",
	"vertical_scroller",
	"vertical_tile",
	"vertical_grid",
	"vertical_deck",
	"right_tile",
	"dwindle",
	"fair",
	"vertical_fair",
}

func (m *Mango) ListLayouts() ([]ipc.Layout, error) {
	var response mangoLayouts
	source := ipc.LayoutSourceDynamic
	if err := m.query("get layouts", &response); err != nil {
		source = ipc.LayoutSourceStatic
		response.Layouts = make([]mangoLayout, 0, len(mangoKnownLayouts))
		for _, name := range mangoKnownLayouts {
			response.Layouts = append(response.Layouts, mangoLayout{Name: name})
		}
	}
	active := mangoActiveLayoutIndex(m)
	out := make([]ipc.Layout, 0, len(response.Layouts))
	for i, layout := range response.Layouts {
		out = append(out, ipc.Layout{
			Name:    layout.Name,
			Current: i == active,
			Source:  source,
		})
	}
	return out, nil
}

func mangoActiveLayoutIndex(m *Mango) int {
	var response mangoMonitors
	if err := m.query("get all-monitors", &response); err != nil {
		return -1
	}
	for _, monitor := range response.Monitors {
		if monitor.Active.value() {
			return monitor.LayoutIndex
		}
	}
	if len(response.Monitors) > 0 {
		return response.Monitors[0].LayoutIndex
	}
	return -1
}

func (m *Mango) GetConfig(key string) (interface{}, error) {
	return nil, ipc.ErrNotSupported
}

func (m *Mango) SetConfig(key string, value interface{}) error {
	v := mangoValueString(value)
	switch key {
	case "gaps.inner":
		if err := m.setOption("gap_inner_horizontal", v); err != nil {
			return err
		}
		return m.setOption("gap_inner_vertical", v)
	case "gaps.outer":
		if err := m.setOption("gap_outer_horizontal", v); err != nil {
			return err
		}
		return m.setOption("gap_outer_vertical", v)
	case "border.width":
		return m.setOption("border_px", v)
	case "border.active_color":
		return m.setOption("focus_color", ipc.MangoColor(v))
	case "border.inactive_color":
		return m.setOption("border_color", ipc.MangoColor(v))
	case "border.rounding", "rounding", "border_radius":
		return m.setOption("border_radius", v)
	case "opacity.active":
		return m.setOption("focused_opacity", v)
	case "opacity.inactive":
		return m.setOption("unfocused_opacity", v)
	case "blur.enabled":
		return m.setOption("blur", v)
	case "blur.size":
		return m.setOption("blur_params_radius", v)
	case "blur.passes":
		return m.setOption("blur_params_num_passes", v)
	case "blur.brightness":
		return m.setOption("blur_params_brightness", v)
	case "blur.contrast":
		return m.setOption("blur_params_contrast", v)
	case "blur.saturation":
		return m.setOption("blur_params_saturation", v)
	case "shadows", "shadow.enabled":
		return m.setOption("shadows", v)
	case "shadow.size":
		return m.setOption("shadows_size", v)
	case "shadow.color":
		return m.setOption("shadows_color", ipc.MangoColor(v))
	case "animations.enabled":
		return m.setOption("animations", v)
	default:
		return ipc.ErrNotSupported
	}
}

func mangoValueString(value interface{}) string {
	switch v := value.(type) {
	case bool:
		if v {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprint(value)
	}
}

func (m *Mango) setOption(key, value string) error {
	if key == "" || strings.ContainsAny(key+value, "\r\n") {
		return fmt.Errorf("invalid Mango option")
	}
	return m.dispatch("setoption", []string{key, value}, "")
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
	if path == "" {
		return m.dispatch("reload_config", nil, "")
	}
	if strings.Contains(path, ",") {
		return fmt.Errorf("Mango config path cannot contain commas")
	}
	return m.dispatch("load_config_file", []string{path}, "")
}

func (m *Mango) GetAnimations() (interface{}, error) { return nil, ipc.ErrNotSupported }

func (m *Mango) GetCursorPosition() (int, int, error) {
	var cur mangoCursorPos
	if err := m.query("get cursorpos", &cur); err != nil {
		return 0, 0, err
	}
	return int(math.Round(cur.X)), int(math.Round(cur.Y)), nil
}

func (m *Mango) BindKey(mods, key, command string) error { return ipc.ErrNotSupported }
func (m *Mango) UnbindKey(mods, key string) error        { return ipc.ErrNotSupported }

func (m *Mango) Execute(command string) error {
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("command is required")
	}
	return m.dispatch("spawn_shell", []string{command}, "")
}

func (m *Mango) Exit() error {
	return m.dispatch("quit", nil, "")
}

func (m *Mango) SwitchKeyboardLayout(action string) error {
	if strings.EqualFold(action, "next") || action == "" {
		return m.dispatch("switch_keyboard_layout", []string{"0"}, "")
	}
	if strings.EqualFold(action, "prev") || strings.EqualFold(action, "previous") {
		return ipc.ErrNotSupported
	}
	index, err := strconv.Atoi(action)
	if err != nil || index < 1 || index > 100 {
		return fmt.Errorf("invalid keyboard layout action %q", action)
	}
	return m.dispatch("switch_keyboard_layout", []string{strconv.Itoa(index)}, "")
}

func (m *Mango) SetKeyboardLayouts(layouts string, variants string) error {
	if strings.ContainsAny(layouts+variants, "\r\n") {
		return fmt.Errorf("keyboard layouts and variants cannot contain newlines")
	}
	if err := m.setOption("xkb_rules_layout", layouts); err != nil {
		return err
	}
	return m.setOption("xkb_rules_variant", variants)
}

func (m *Mango) Subscribe() (<-chan ipc.Event, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, fmt.Errorf("mango: connection closed")
	}
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
	if m.closed {
		m.subscribed = false
		m.mu.Unlock()
		ws.Close()
		close(ch)
		return nil, fmt.Errorf("mango: connection closed")
	}
	m.watch = ws
	m.mu.Unlock()
	go m.forwardWatchEvents(ch, ws)

	return ch, nil
}

func (m *Mango) GetCapabilities() (ipc.Capabilities, error) {
	return ipc.Capabilities{
		Blur:                true,
		Shadows:             true,
		Animations:          true,
		RoundedCorners:      true,
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

func mangoDirection(dir string) (string, error) {
	dir = normalizeDirection(dir)
	switch dir {
	case "left", "right", "up", "down":
		return dir, nil
	default:
		return "", fmt.Errorf("invalid direction %q", dir)
	}
}
