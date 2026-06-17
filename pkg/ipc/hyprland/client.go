package hyprland

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"axctl/pkg/ipc"
)

type Hyprland struct {
	signature      string
	mu             sync.Mutex
	versionMu      sync.Mutex
	versionKnown   bool
	useLuaDispatch bool
}

func New() (*Hyprland, error) {
	sig := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
	if sig == "" {
		return nil, fmt.Errorf("HYPRLAND_INSTANCE_SIGNATURE not set")
	}
	return &Hyprland{signature: sig}, nil
}

func (h *Hyprland) getSocketPath(socketName string) string {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	return fmt.Sprintf("%s/hypr/%s/%s", runtimeDir, h.signature, socketName)
}

var dispatchErrPrefixes = []string{
	"error:",
	"unknown request",
	"Bad ",
	"Invalid dispatcher",
	"Parse error",
}

func (h *Hyprland) dispatch(cmd string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	conn, err := net.Dial("unix", h.getSocketPath(".socket.sock"))
	if err != nil {
		return "", err
	}
	defer conn.Close()

	if _, err := conn.Write([]byte(cmd)); err != nil {
		return "", err
	}

	response, err := io.ReadAll(conn)
	if err != nil {
		return "", err
	}
	resp := string(response)
	trimmed := strings.TrimSpace(resp)
	for _, prefix := range dispatchErrPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return resp, fmt.Errorf("hyprland rejected request: %s", trimmed)
		}
	}
	return resp, nil
}

func (h *Hyprland) supportsLuaDispatchers() bool {
	h.versionMu.Lock()
	if h.versionKnown {
		useLuaDispatch := h.useLuaDispatch
		h.versionMu.Unlock()
		return useLuaDispatch
	}
	h.versionMu.Unlock()

	// Use hyprctl binary instead of IPC socket to query version,
	// since the socket doesn't support the version command directly.
	out, err := exec.Command("hyprctl", "version").Output()
	if err != nil {
		return false
	}

	version, err := parseHyprlandVersion(string(out))
	if err != nil {
		return false
	}

	useLuaDispatch := isHyprlandVersionAtLeast(version, 0, 55)
	h.versionMu.Lock()
	h.versionKnown = true
	h.useLuaDispatch = useLuaDispatch
	h.versionMu.Unlock()
	return useLuaDispatch
}

func (h *Hyprland) dispatchVersioned(legacy, lua string) (string, error) {
	if h.supportsLuaDispatchers() {
		resp, err := h.dispatch("[[BATCH]]dispatch " + lua)
		if err == nil {
			return resp, nil
		}
		// Lua dispatcher failed (e.g. "Invalid dispatcher" on mainline),
		// fall back to legacy dispatcher.
		h.versionMu.Lock()
		h.useLuaDispatch = false
		h.versionMu.Unlock()
	}
	return h.dispatch("[[BATCH]]dispatch " + legacy)
}

func parseHyprlandVersion(resp string) (string, error) {
	trimmed := strings.TrimSpace(resp)
	if strings.HasPrefix(trimmed, "{") {
		var data struct {
			Version string `json:"version"`
			Tag     string `json:"tag"`
		}
		if err := json.Unmarshal([]byte(trimmed), &data); err != nil {
			return "", err
		}
		if data.Version != "" {
			return data.Version, nil
		}
		if data.Tag != "" {
			return data.Tag, nil
		}
		return "", fmt.Errorf("hyprland version response did not include version")
	}

	fields := strings.Fields(trimmed)
	for _, field := range fields {
		field = strings.TrimPrefix(field, "v")
		if _, err := parseVersionNumber(field); err == nil {
			return field, nil
		}
	}
	return "", fmt.Errorf("could not parse hyprland version from %q", trimmed)
}

func isHyprlandVersionAtLeast(version string, major, minor int) bool {
	parsed, err := parseVersionNumber(version)
	if err != nil {
		return false
	}
	if parsed.major != major {
		return parsed.major > major
	}
	return parsed.minor >= minor
}

type versionNumber struct {
	major int
	minor int
}

func parseVersionNumber(version string) (versionNumber, error) {
	clean := strings.TrimPrefix(strings.TrimSpace(version), "v")
	parts := strings.Split(clean, ".")
	if len(parts) < 2 {
		return versionNumber{}, fmt.Errorf("invalid version %q", version)
	}

	major, err := strconv.Atoi(versionDigits(parts[0]))
	if err != nil {
		return versionNumber{}, err
	}
	minor, err := strconv.Atoi(versionDigits(parts[1]))
	if err != nil {
		return versionNumber{}, err
	}
	return versionNumber{major: major, minor: minor}, nil
}

func versionDigits(value string) string {
	for i, r := range value {
		if r < '0' || r > '9' {
			return value[:i]
		}
	}
	return value
}

func hyprTarget(id string) string {
	if id == "" {
		return ""
	}
	return "address:" + id
}

func luaTargetField(id string) string {
	if id == "" {
		return ""
	}
	return fmt.Sprintf(", window = %q", hyprTarget(id))
}

func luaDirection(direction string) string {
	switch direction {
	case "l":
		return "left"
	case "r":
		return "right"
	case "u":
		return "up"
	case "d":
		return "down"
	default:
		return direction
	}
}

func (h *Hyprland) ListWindows() ([]ipc.Window, error) {
	resp, err := h.dispatch("j/clients")
	if err != nil {
		return nil, err
	}

	if resp == "" || resp == "[]" {
		return []ipc.Window{}, nil
	}

	var clients []struct {
		Address    string `json:"address"`
		Title      string `json:"title"`
		Class      string `json:"class"`
		Floating   bool   `json:"floating"`
		Fullscreen int    `json:"fullscreen"`
		Pinned     bool   `json:"pinned"`
		Monitor    int    `json:"monitor"`
		At         []int  `json:"at"`
		Size       []int  `json:"size"`
		Workspace  struct {
			ID int `json:"id"`
		} `json:"workspace"`
	}

	if err := json.Unmarshal([]byte(resp), &clients); err != nil {
		fmt.Printf("[Hyprland] Unmarshal error: %v | Raw: %s\n", err, resp)
		return nil, err
	}

	windows := make([]ipc.Window, len(clients))
	for i, c := range clients {
		windows[i] = ipc.Window{
			ID:           c.Address,
			Title:        c.Title,
			AppID:        c.Class,
			WorkspaceID:  fmt.Sprintf("%d", c.Workspace.ID),
			IsFocused:    false, // Will be updated if active
			IsFloating:   c.Floating,
			IsFullscreen: c.Fullscreen != 0,
			IsHidden:     false,
			Metadata: map[string]interface{}{
				"monitor_id": fmt.Sprintf("%d", c.Monitor),
				"pinned":     c.Pinned,
				"x":          c.At[0],
				"y":          c.At[1],
				"width":      c.Size[0],
				"height":     c.Size[1],
			},
		}
	}
	return windows, nil
}

func (h *Hyprland) ActiveWindow() (string, error) {
	resp, err := h.dispatch("j/activewindow")
	if err != nil {
		return "", err
	}
	var active struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal([]byte(resp), &active); err != nil {
		return "", err
	}
	return active.Address, nil
}

func (h *Hyprland) FocusWindow(id string) error {
	_, err := h.dispatchVersioned(
		fmt.Sprintf("focuswindow %s", hyprTarget(id)),
		fmt.Sprintf("hl.dsp.focus({ window = %q })", hyprTarget(id)),
	)
	return err
}

func (h *Hyprland) FocusDir(direction string) error {
	_, err := h.dispatchVersioned(
		fmt.Sprintf("movefocus %s", direction),
		fmt.Sprintf("hl.dsp.focus({ direction = %q })", luaDirection(direction)),
	)
	return err
}

func (h *Hyprland) CloseWindow(id string) error {
	target := hyprTarget(id)
	lua := "hl.dsp.window.close()"
	if target != "" {
		lua = fmt.Sprintf("hl.dsp.window.close({ window = %q })", target)
	}
	_, err := h.dispatchVersioned(fmt.Sprintf("closewindow %s", target), lua)
	return err
}

func (h *Hyprland) MoveWindow(id string, direction string) error {
	arg := direction
	if id != "" {
		arg = direction + "," + hyprTarget(id)
	}
	_, err := h.dispatchVersioned(
		fmt.Sprintf("movewindow %s", arg),
		fmt.Sprintf("hl.dsp.window.move({ direction = %q%s })", luaDirection(direction), luaTargetField(id)),
	)
	return err
}

func (h *Hyprland) ResizeWindow(id string, width, height int) error {
	target := hyprTarget(id)
	_, err := h.dispatchVersioned(
		fmt.Sprintf("resizewindowpixel exact %d %d,%s", width, height, target),
		fmt.Sprintf("hl.dsp.window.resize({ x = %d, y = %d%s })", width, height, luaTargetField(id)),
	)
	return err
}

func (h *Hyprland) ToggleFloating(id string) error {
	target := hyprTarget(id)
	_, err := h.dispatchVersioned(
		fmt.Sprintf("togglefloating %s", target),
		fmt.Sprintf("hl.dsp.window.float({ action = \"toggle\"%s })", luaTargetField(id)),
	)
	return err
}

func (h *Hyprland) SetFullscreen(id string, state bool) error {
	windows, err := h.ListWindows()
	if err != nil {
		return err
	}

	targetID := id
	if targetID == "" {
		targetID, _ = h.ActiveWindow()
	}

	isFs := false
	for _, w := range windows {
		if w.ID == targetID {
			isFs = w.IsFullscreen
			break
		}
	}

	if isFs != state {
		_, err := h.dispatchVersioned(
			"fullscreen 0",
			"hl.dsp.window.fullscreen({ mode = \"fullscreen\", action = \"toggle\" })",
		)
		return err
	}
	return nil
}

func (h *Hyprland) SetMaximized(id string, state bool) error {
	val := "0"
	if state {
		val = "1"
	}
	action := "unset"
	if state {
		action = "set"
	}
	_, err := h.dispatchVersioned(
		fmt.Sprintf("fullscreen %s", val),
		fmt.Sprintf("hl.dsp.window.fullscreen({ mode = \"maximized\", action = %q })", action),
	)
	return err
}

func (h *Hyprland) PinWindow(id string, state bool) error {
	target := hyprTarget(id)
	_, err := h.dispatchVersioned(
		fmt.Sprintf("pin %s", target),
		fmt.Sprintf("hl.dsp.window.pin({ action = \"toggle\"%s })", luaTargetField(id)),
	)
	return err
}

func (h *Hyprland) ToggleGroup(id string) error {
	_, err := h.dispatchVersioned("togglegroup", "hl.dsp.group.toggle()")
	return err
}

func (h *Hyprland) GroupNav(direction string) error {
	dir := "f"
	if direction == "l" || direction == "u" || direction == "b" {
		dir = "b"
	}
	luaDir := "next"
	if dir == "b" {
		luaDir = "prev"
	}
	_, err := h.dispatchVersioned(
		fmt.Sprintf("changegroupactive %s", dir),
		fmt.Sprintf("hl.dsp.group.%s()", luaDir),
	)
	return err
}

func (h *Hyprland) SetLayoutProperty(id string, key, value string) error {
	return ipc.ErrNotSupported
}

func (h *Hyprland) ListWorkspaces() ([]ipc.Workspace, error) {
	resp, err := h.dispatch("j/workspaces")
	if err != nil {
		return nil, err
	}

	var workspaces []struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		Monitor string `json:"monitor"`
	}

	if err := json.Unmarshal([]byte(resp), &workspaces); err != nil {
		return nil, err
	}

	activeResp, _ := h.dispatch("j/activeworkspace")
	var activeWS struct {
		ID int `json:"id"`
	}
	if activeResp != "" {
		json.Unmarshal([]byte(activeResp), &activeWS)
	}

	res := make([]ipc.Workspace, len(workspaces))
	for i, w := range workspaces {
		res[i] = ipc.Workspace{
			ID:        fmt.Sprintf("%d", w.ID),
			Name:      w.Name,
			MonitorID: w.Monitor,
			IsActive:  w.ID == activeWS.ID,
			IsEmpty:   false, // Not directly available from basic j/workspaces without parsing windows
			Metadata: map[string]interface{}{
				"focused": w.ID == activeWS.ID,
			},
		}
	}
	return res, nil
}

func (h *Hyprland) ActiveWorkspace() (*ipc.Workspace, error) {
	resp, err := h.dispatch("j/activeworkspace")
	if err != nil {
		return nil, err
	}
	var ws struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		Monitor string `json:"monitor"`
	}
	if err := json.Unmarshal([]byte(resp), &ws); err != nil {
		return nil, err
	}
	return &ipc.Workspace{
		ID:        fmt.Sprintf("%d", ws.ID),
		Name:      ws.Name,
		MonitorID: ws.Monitor,
		IsActive:  true,
		IsEmpty:   false,
		Metadata: map[string]interface{}{
			"focused": true,
		},
	}, nil
}

func (h *Hyprland) SwitchWorkspace(id string) error {
	_, err := h.dispatchVersioned(
		fmt.Sprintf("workspace %s", id),
		fmt.Sprintf("hl.dsp.focus({ workspace = %q })", id),
	)
	return err
}

func (h *Hyprland) MoveToWorkspace(windowID, workspaceID string) error {
	target := hyprTarget(windowID)
	_, err := h.dispatchVersioned(
		fmt.Sprintf("movetoworkspace %s,%s", workspaceID, target),
		fmt.Sprintf("hl.dsp.window.move({ workspace = %q%s })", workspaceID, luaTargetField(windowID)),
	)
	return err
}

func (h *Hyprland) ListMonitors() ([]ipc.Monitor, error) {
	resp, err := h.dispatch("j/monitors")
	if err != nil {
		return nil, err
	}

	var monitors []struct {
		ID              int     `json:"id"`
		Name            string  `json:"name"`
		Width           int     `json:"width"`
		Height          int     `json:"height"`
		RefreshRate     float64 `json:"refreshRate"`
		Focused         bool    `json:"focused"`
		Scale           float64 `json:"scale"`
		X               int     `json:"x"`
		Y               int     `json:"y"`
		Transform       int     `json:"transform"`
		ActiveWorkspace struct {
			Name string `json:"name"`
		} `json:"activeWorkspace"`
	}

	if err := json.Unmarshal([]byte(resp), &monitors); err != nil {
		return nil, err
	}

	res := make([]ipc.Monitor, len(monitors))
	for i, m := range monitors {
		res[i] = ipc.Monitor{
			ID:          fmt.Sprintf("%d", m.ID),
			Name:        m.Name,
			Description: "",
			Width:       m.Width,
			Height:      m.Height,
			RefreshRate: m.RefreshRate,
			Scale:       m.Scale,
			IsFocused:   m.Focused,
			Metadata: map[string]interface{}{
				"active_workspace": m.ActiveWorkspace.Name,
				"x":                m.X,
				"y":                m.Y,
				"transform":        m.Transform,
			},
		}
	}
	return res, nil
}

func (h *Hyprland) FocusMonitor(id string) error {
	_, err := h.dispatchVersioned(
		fmt.Sprintf("focusmonitor %s", id),
		fmt.Sprintf("hl.dsp.focus({ monitor = %q })", id),
	)
	return err
}

func (h *Hyprland) MoveToMonitor(windowID, monitorID string) error {
	target := hyprTarget(windowID)
	_, err := h.dispatchVersioned(
		fmt.Sprintf("movewindowmon %s,%s", monitorID, target),
		fmt.Sprintf("hl.dsp.window.move({ monitor = %q%s })", monitorID, luaTargetField(windowID)),
	)
	return err
}

func (h *Hyprland) SetDpms(monitorID string, on bool) error {
	state := "off"
	if on {
		state = "on"
	}
	if monitorID != "" {
		_, err := h.dispatchVersioned(
			fmt.Sprintf("dpms %s %s", state, monitorID),
			fmt.Sprintf("hl.dsp.dpms({ action = %q, monitor = %q })", state, monitorID),
		)
		return err
	}
	_, err := h.dispatchVersioned(
		fmt.Sprintf("dpms %s", state),
		fmt.Sprintf("hl.dsp.dpms({ action = %q })", state),
	)
	return err
}

func (h *Hyprland) SetLayout(name string) error {
	_, err := h.dispatch(fmt.Sprintf("keyword general:layout %s", name))
	return err
}

func (h *Hyprland) MoveWindowPixel(id string, x, y int) error {
	target := hyprTarget(id)
	_, err := h.dispatchVersioned(
		fmt.Sprintf("movewindowpixel exact %d %d,%s", x, y, target),
		fmt.Sprintf("hl.dsp.window.move({ x = %d, y = %d%s })", x, y, luaTargetField(id)),
	)
	return err
}

func (h *Hyprland) MoveToWorkspaceSilent(windowID, workspaceID string) error {
	target := hyprTarget(windowID)
	_, err := h.dispatchVersioned(
		fmt.Sprintf("movetoworkspacesilent %s,%s", workspaceID, target),
		fmt.Sprintf("hl.dsp.window.move({ workspace = %q, follow = false%s })", workspaceID, luaTargetField(windowID)),
	)
	return err
}

func (h *Hyprland) ToggleSpecialWorkspace(name string) error {
	if name == "" {
		_, err := h.dispatchVersioned("togglespecialworkspace", "hl.dsp.workspace.toggle_special()")
		return err
	}
	_, err := h.dispatchVersioned(
		fmt.Sprintf("togglespecialworkspace %s", name),
		fmt.Sprintf("hl.dsp.workspace.toggle_special(%q)", name),
	)
	return err
}

func (h *Hyprland) GetConfig(key string) (interface{}, error) {
	resp, err := h.dispatch(fmt.Sprintf("j/getoption %s", key))
	if err != nil {
		return nil, err
	}
	var data interface{}
	if err := json.Unmarshal([]byte(resp), &data); err != nil {
		return nil, err
	}
	return data, nil
}

func (h *Hyprland) BatchConfig(configs map[string]interface{}) error {
	var cmds []string

	mapping := map[string]string{
		"gaps.inner":            "general:gaps_in",
		"gaps.outer":            "general:gaps_out",
		"border.width":          "general:border_size",
		"border.active_color":   "general:col.active_border",
		"border.inactive_color": "general:col.inactive_border",
		"opacity.active":        "decoration:active_opacity",
		"opacity.inactive":      "decoration:inactive_opacity",
		"blur.enabled":          "decoration:blur:enabled",
		"blur.size":             "decoration:blur:size",
		"blur.passes":           "decoration:blur:passes",
	}

	for k, v := range configs {
		hyprKey := k
		if mapped, ok := mapping[k]; ok {
			hyprKey = mapped
		}
		cmds = append(cmds, fmt.Sprintf("keyword %s %v", hyprKey, v))
	}
	_, err := h.dispatch(fmt.Sprintf("[[BATCH]]%s", strings.Join(cmds, ";")))
	return err
}

func (h *Hyprland) BatchKeybinds(jsonPayload string) error {
	var payload ipc.BatchKeybindsPayload
	if err := json.Unmarshal([]byte(jsonPayload), &payload); err != nil {
		return fmt.Errorf("invalid keybinds payload: %w", err)
	}

	var cmds []string

	// Process unbinds first
	for _, u := range payload.Unbinds {
		mods := strings.Join(u.Modifiers, " ")
		cmds = append(cmds, fmt.Sprintf("keyword unbind %s,%s", mods, u.Key))
	}

	// Process binds
	for _, b := range payload.Binds {
		mods := strings.Join(b.Modifiers, " ")
		bindKeyword := "bind"
		if b.Flags != "" {
			bindKeyword = "bind" + b.Flags
		}
		if b.Flags == "m" && b.Argument == "" {
			cmds = append(cmds, fmt.Sprintf("keyword %s %s,%s,%s", bindKeyword, mods, b.Key, b.Dispatcher))
		} else {
			cmds = append(cmds, fmt.Sprintf("keyword %s %s,%s,%s,%s", bindKeyword, mods, b.Key, b.Dispatcher, b.Argument))
		}
	}

	if len(cmds) == 0 {
		return nil
	}

	_, err := h.dispatch("[[BATCH]]" + strings.Join(cmds, ";"))
	return err
}

func (h *Hyprland) RawBatch(command string) error {
	_, err := h.dispatch("[[BATCH]]" + command)
	return err
}

func (h *Hyprland) GetAnimations() (interface{}, error) {
	resp, err := h.dispatch("j/animations")
	if err != nil {
		return nil, err
	}
	var data interface{}
	if err := json.Unmarshal([]byte(resp), &data); err != nil {
		return nil, err
	}
	return data, nil
}

func (h *Hyprland) GetCursorPosition() (int, int, error) {
	resp, err := h.dispatch("j/cursorpos")
	if err != nil {
		return 0, 0, err
	}
	var pos struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	if err := json.Unmarshal([]byte(resp), &pos); err != nil {
		return 0, 0, err
	}
	return pos.X, pos.Y, nil
}

func (h *Hyprland) BindKey(mods, key, command string) error {
	_, err := h.dispatch(fmt.Sprintf("keyword bind %s,%s,%s", mods, key, command))
	return err
}

func (h *Hyprland) UnbindKey(mods, key string) error {
	_, err := h.dispatch(fmt.Sprintf("keyword unbind %s,%s", mods, key))
	return err
}

func (h *Hyprland) SetConfig(key string, value interface{}) error {
	mapping := map[string]string{
		"gaps.inner":            "general:gaps_in",
		"gaps.outer":            "general:gaps_out",
		"border.width":          "general:border_size",
		"border.active_color":   "general:col.active_border",
		"border.inactive_color": "general:col.inactive_border",
		"opacity.active":        "decoration:active_opacity",
		"opacity.inactive":      "decoration:inactive_opacity",
		"blur.enabled":          "decoration:blur:enabled",
		"blur.size":             "decoration:blur:size",
		"blur.passes":           "decoration:blur:passes",
	}

	hyprKey, ok := mapping[key]
	if !ok {
		hyprKey = key
	}

	_, err := h.dispatch(fmt.Sprintf("keyword %s %v", hyprKey, value))
	return err
}

func (h *Hyprland) ReloadConfig() error {
	_, err := h.dispatch("reload")
	return err
}

func (h *Hyprland) Execute(command string) error {
	_, err := h.dispatchVersioned(
		fmt.Sprintf("exec %s", command),
		fmt.Sprintf("hl.dsp.exec_cmd(%q)", command),
	)
	return err
}

func (h *Hyprland) Exit() error {
	_, err := h.dispatchVersioned("exit", "hl.dsp.exit()")
	return err
}

func (h *Hyprland) Subscribe() (<-chan ipc.Event, error) {
	conn, err := net.Dial("unix", h.getSocketPath(".socket2.sock"))
	if err != nil {
		return nil, err
	}

	ch := make(chan ipc.Event)
	go func() {
		defer conn.Close()
		defer close(ch)
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.SplitN(line, ">>", 2)
			if len(parts) < 2 {
				continue
			}

			event := ipc.Event{
				Timestamp: time.Now().Unix(),
				Payload:   make(map[string]interface{}),
			}

			switch parts[0] {
			case "openwindow":
				event.Type = ipc.EventWindowCreated
				data := strings.SplitN(parts[1], ",", 4)
				if len(data) >= 4 {
					event.Window = &ipc.Window{
						ID:          "0x" + data[0],
						WorkspaceID: data[1],
						AppID:       data[2],
						Title:       data[3],
					}
				}
			case "closewindow":
				event.Type = ipc.EventWindowClosed
				event.Payload["address"] = "0x" + parts[1]
			case "activewindow":
				event.Type = ipc.EventWindowFocused
				data := strings.SplitN(parts[1], ",", 2)
				if len(data) >= 2 {
					event.Payload["class"] = data[0]
					event.Payload["title"] = data[1]
				}
			case "activewindowv2":
				event.Type = ipc.EventWindowFocused
				addr := strings.TrimSpace(parts[1])
				if addr != "" {
					event.Payload["address"] = "0x" + addr
				}
			case "workspace":
				event.Type = ipc.EventWorkspaceChanged
				event.Payload["name"] = parts[1]
			case "movewindow":
				data := strings.SplitN(parts[1], ",", 2)
				if len(data) >= 2 {
					event.Type = ipc.EventWindowMoved
					event.Payload["address"] = "0x" + data[0]
					event.Payload["workspace"] = data[1]
				}
			case "changefloatingmode", "floating":
				data := strings.SplitN(parts[1], ",", 2)
				if len(data) >= 2 {
					event.Payload["address"] = "0x" + data[0]
					event.Payload["floating"] = data[1] == "1"
				}
			case "fullscreen":
				event.Type = ipc.EventFullscreenChanged
				event.Payload["fullscreen"] = parts[1] == "1"
			case "monitoradded":
				event.Type = ipc.EventMonitorChanged
				event.Payload["monitor"] = parts[1]
				event.Payload["action"] = "added"
			case "monitorremoved":
				event.Type = ipc.EventMonitorChanged
				event.Payload["monitor"] = parts[1]
				event.Payload["action"] = "removed"
			case "configreloaded":
				event.Type = ipc.EventConfigReloaded
			case "focusedmon":
				event.Type = ipc.EventFocusedMonitorChanged
				data := strings.SplitN(parts[1], ",", 2)
				if len(data) >= 2 {
					event.Payload["monitor"] = data[0]
					event.Payload["workspace"] = data[1]
				}
			case "windowtitle":
				event.Type = ipc.EventWindowTitleChanged
				event.Payload["address"] = "0x" + parts[1]
			}

			if event.Type != "" || len(event.Payload) > 0 {
				ch <- event
			}
		}
	}()

	return ch, nil
}

func (h *Hyprland) SwitchKeyboardLayout(action string) error {
	cmd := fmt.Sprintf("switchxkblayout current %s", action)
	_, err := h.dispatch(cmd)
	return err
}

func (h *Hyprland) SetKeyboardLayouts(layouts string, variants string) error {
	if _, err := h.dispatch(fmt.Sprintf("keyword input:kb_layout %s", layouts)); err != nil {
		return err
	}
	if variants != "" {
		if _, err := h.dispatch(fmt.Sprintf("keyword input:kb_variant %s", variants)); err != nil {
			return err
		}
	} else {
		h.dispatch("keyword input:kb_variant ") // clear
	}
	return nil
}

func (h *Hyprland) GetCapabilities() (ipc.Capabilities, error) {
	return ipc.Capabilities{
		Blur:                true,
		Shadows:             true,
		Animations:          true,
		RoundedCorners:      true,
		WorkspacesSupported: true,
		WindowsSupported:    true,
	}, nil
}
