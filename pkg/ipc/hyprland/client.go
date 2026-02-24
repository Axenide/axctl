package hyprland

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"axctl/pkg/ipc"
)

type Hyprland struct {
	signature string
	mu        sync.Mutex
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
	return string(response), nil
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
			ID:          c.Address,
			Title:       c.Title,
			Class:       c.Class,
			WorkspaceID: fmt.Sprintf("%d", c.Workspace.ID),
			MonitorID:   fmt.Sprintf("%d", c.Monitor),
			Floating:    c.Floating,
			Fullscreen:  c.Fullscreen != 0,
			Pinned:      c.Pinned,
			X:           c.At[0],
			Y:           c.At[1],
			Width:       c.Size[0],
			Height:      c.Size[1],
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
	_, err := h.dispatch(fmt.Sprintf("dispatch focuswindow address:%s", id))
	return err
}

func (h *Hyprland) FocusDir(direction string) error {
	_, err := h.dispatch(fmt.Sprintf("dispatch movefocus %s", direction))
	return err
}

func (h *Hyprland) CloseWindow(id string) error {
	target := "address:" + id
	if id == "" {
		target = ""
	}
	_, err := h.dispatch(fmt.Sprintf("dispatch closewindow %s", target))
	return err
}

func (h *Hyprland) MoveWindow(id string, direction string) error {
	arg := direction
	if id != "" {
		arg = direction + ",address:" + id
	}
	_, err := h.dispatch(fmt.Sprintf("dispatch movewindow %s", arg))
	return err
}

func (h *Hyprland) ResizeWindow(id string, width, height int) error {
	target := "address:" + id
	if id == "" {
		target = ""
	}
	_, err := h.dispatch(fmt.Sprintf("dispatch resizewindowpixel exact %d %d,%s", width, height, target))
	return err
}

func (h *Hyprland) ToggleFloating(id string) error {
	target := "address:" + id
	if id == "" {
		target = ""
	}
	_, err := h.dispatch(fmt.Sprintf("dispatch togglefloating %s", target))
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
			isFs = w.Fullscreen
			break
		}
	}

	if isFs != state {
		_, err := h.dispatch("dispatch fullscreen 0")
		return err
	}
	return nil
}

func (h *Hyprland) SetMaximized(id string, state bool) error {
	val := "0"
	if state {
		val = "1"
	}
	_, err := h.dispatch(fmt.Sprintf("dispatch fullscreen %s", val))
	return err
}

func (h *Hyprland) PinWindow(id string, state bool) error {
	target := "address:" + id
	if id == "" {
		target = ""
	}
	_, err := h.dispatch(fmt.Sprintf("dispatch pin %s", target))
	return err
}

func (h *Hyprland) ToggleGroup(id string) error {
	_, err := h.dispatch("dispatch togglegroup")
	return err
}

func (h *Hyprland) GroupNav(direction string) error {
	dir := "f"
	if direction == "l" || direction == "u" || direction == "b" {
		dir = "b"
	}
	_, err := h.dispatch(fmt.Sprintf("dispatch changegroupactive %s", dir))
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
			Active:    w.ID == activeWS.ID,
			Focused:   w.ID == activeWS.ID,
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
		Active:    true,
		Focused:   true,
	}, nil
}

func (h *Hyprland) SwitchWorkspace(id string) error {
	_, err := h.dispatch(fmt.Sprintf("dispatch workspace %s", id))
	return err
}

func (h *Hyprland) MoveToWorkspace(windowID, workspaceID string) error {
	target := "address:" + windowID
	if windowID == "" {
		target = ""
	}
	_, err := h.dispatch(fmt.Sprintf("dispatch movetoworkspace %s,%s", workspaceID, target))
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
			ID:        fmt.Sprintf("%d", m.ID),
			Name:      m.Name,
			Width:     m.Width,
			Height:    m.Height,
			Refresh:   m.RefreshRate,
			Active:    m.Focused,
			Workspace: m.ActiveWorkspace.Name,
			Scale:     m.Scale,
			X:         m.X,
			Y:         m.Y,
			Transform: m.Transform,
		}
	}
	return res, nil
}

func (h *Hyprland) FocusMonitor(id string) error {
	_, err := h.dispatch(fmt.Sprintf("dispatch focusmonitor %s", id))
	return err
}

func (h *Hyprland) MoveToMonitor(windowID, monitorID string) error {
	target := "address:" + windowID
	if windowID == "" {
		target = ""
	}
	_, err := h.dispatch(fmt.Sprintf("dispatch movewindowmon %s,%s", monitorID, target))
	return err
}

func (h *Hyprland) SetDpms(monitorID string, on bool) error {
	state := "off"
	if on {
		state = "on"
	}
	if monitorID != "" {
		_, err := h.dispatch(fmt.Sprintf("dispatch dpms %s %s", state, monitorID))
		return err
	}
	_, err := h.dispatch(fmt.Sprintf("dispatch dpms %s", state))
	return err
}

func (h *Hyprland) SetLayout(name string) error {
	_, err := h.dispatch(fmt.Sprintf("keyword general:layout %s", name))
	return err
}

func (h *Hyprland) MoveWindowPixel(id string, x, y int) error {
	target := "address:" + id
	if id == "" {
		target = ""
	}
	_, err := h.dispatch(fmt.Sprintf("dispatch movewindowpixel exact %d %d,%s", x, y, target))
	return err
}

func (h *Hyprland) MoveToWorkspaceSilent(windowID, workspaceID string) error {
	target := "address:" + windowID
	if windowID == "" {
		target = ""
	}
	_, err := h.dispatch(fmt.Sprintf("dispatch movetoworkspacesilent %s,%s", workspaceID, target))
	return err
}

func (h *Hyprland) ToggleSpecialWorkspace(name string) error {
	_, err := h.dispatch(fmt.Sprintf("dispatch togglespecialworkspace %s", name))
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
	_, err := h.dispatch(fmt.Sprintf("dispatch exec %s", command))
	return err
}

func (h *Hyprland) Exit() error {
	_, err := h.dispatch("exit")
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
						Class:       data[2],
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
			case "floating":
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
