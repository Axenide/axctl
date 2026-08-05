package niri

import (
	"encoding/json"
	"errors"
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
}

func New() (*Niri, error) {
	path := os.Getenv("NIRI_SOCKET")
	if path == "" {
		return nil, fmt.Errorf("NIRI_SOCKET not set")
	}
	return &Niri{socketPath: path}, nil
}

func (n *Niri) dial() (net.Conn, error) {
	return net.Dial("unix", n.socketPath)
}

func (n *Niri) writeRequest(conn net.Conn, req interface{}) error {
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		return err
	}
	if conn, ok := conn.(*net.UnixConn); ok {
		_ = conn.CloseWrite()
	}
	return nil
}

type rawReply struct {
	Ok  json.RawMessage `json:"Ok"`
	Err json.RawMessage `json:"Err"`
}

func readReplyFromDecoder(dec *json.Decoder) (json.RawMessage, error) {
	var reply rawReply
	if err := dec.Decode(&reply); err != nil {
		return nil, err
	}
	if len(reply.Err) > 0 && string(reply.Err) != "null" {
		var msg string
		if uerr := json.Unmarshal(reply.Err, &msg); uerr == nil {
			return nil, fmt.Errorf("niri error: %s", msg)
		}
		return nil, fmt.Errorf("niri error: %s", string(reply.Err))
	}
	if len(reply.Ok) == 0 || string(reply.Ok) == "null" {
		return nil, nil
	}
	return reply.Ok, nil
}

func (n *Niri) readReply(conn net.Conn) (json.RawMessage, error) {
	return readReplyFromDecoder(json.NewDecoder(conn))
}

func (n *Niri) request(req interface{}, resp interface{}) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	conn, err := n.dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := n.writeRequest(conn, req); err != nil {
		return err
	}

	ok, err := readReplyFromDecoder(json.NewDecoder(conn))
	if err != nil {
		return err
	}

	if resp == nil || len(ok) == 0 {
		return nil
	}
	if string(ok) == `"Handled"` {
		return nil
	}
	return json.Unmarshal(ok, resp)
}

func (n *Niri) requestRaw(req interface{}) (json.RawMessage, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	conn, err := n.dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := n.writeRequest(conn, req); err != nil {
		return nil, err
	}
	return readReplyFromDecoder(json.NewDecoder(conn))
}

func unwrapVariant(ok json.RawMessage, variant string) (json.RawMessage, error) {
	if len(ok) == 0 {
		return nil, nil
	}
	if string(ok) == "null" {
		return nil, nil
	}
	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal(ok, &wrapped); err != nil {
		return nil, err
	}
	return wrapped[variant], nil
}

func (n *Niri) requestAction(action interface{}) error {
	return n.request(map[string]interface{}{"Action": action}, nil)
}

func parseUint64ID(id string) (uint64, error) {
	var v uint64
	if id == "" {
		return 0, errors.New("empty id")
	}
	if _, err := fmt.Sscanf(id, "%d", &v); err != nil {
		return 0, fmt.Errorf("invalid id %q: %w", id, err)
	}
	return v, nil
}

func (n *Niri) windowIDField(id string) (map[string]interface{}, error) {
	if id == "" {
		return nil, nil
	}
	v, err := parseUint64ID(id)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": v}, nil
}

func (n *Niri) workspaceReference(workspaceID string) (map[string]interface{}, error) {
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace id required")
	}
	if v, err := parseUint64ID(workspaceID); err == nil {
		return map[string]interface{}{"reference": map[string]interface{}{"Id": v}}, nil
	}
	var idx int
	if _, err := fmt.Sscanf(workspaceID, "%d", &idx); err == nil {
		if idx < 0 || idx > 255 {
			return nil, fmt.Errorf("workspace index out of range: %d", idx)
		}
		return map[string]interface{}{"reference": map[string]interface{}{"Index": idx}}, nil
	}
	return map[string]interface{}{"reference": map[string]interface{}{"Name": workspaceID}}, nil
}

func (n *Niri) ListWindows() ([]ipc.Window, error) {
	workspaces, _ := n.ListWorkspaces()
	wsOutputMap := make(map[string]string)
	for _, ws := range workspaces {
		wsOutputMap[ws.ID] = ws.MonitorID
	}

	raw, err := n.requestRaw("Windows")
	if err != nil {
		return nil, err
	}
	variant, err := unwrapVariant(raw, "Windows")
	if err != nil {
		return nil, err
	}
	var niriWindows []niriWindow
	if err := json.Unmarshal(variant, &niriWindows); err != nil {
		return nil, err
	}

	windows := make([]ipc.Window, len(niriWindows))
	for i, w := range niriWindows {
		title := ""
		if w.Title != nil {
			title = *w.Title
		}
		appID := ""
		if w.AppID != nil {
			appID = *w.AppID
		}
		wsID := ""
		if w.WorkspaceID != nil {
			wsID = fmt.Sprintf("%d", *w.WorkspaceID)
		}
		monitorID := ""
		if wsID != "" {
			monitorID = wsOutputMap[wsID]
		}

		windows[i] = ipc.Window{
			ID:           fmt.Sprintf("%d", w.ID),
			Title:        title,
			AppID:        appID,
			WorkspaceID:  wsID,
			IsFocused:    w.IsFocused,
			IsFloating:   w.IsFloating,
			IsFullscreen: false,
			IsHidden:     false,
			Metadata: map[string]interface{}{
				"monitor_id": monitorID,
				"is_urgent":  w.IsUrgent,
				"pid":        w.PID,
			},
		}
	}
	return windows, nil
}

func (n *Niri) ActiveWindow() (string, error) {
	raw, err := n.requestRaw("FocusedWindow")
	if err != nil {
		return "", err
	}
	variant, err := unwrapVariant(raw, "FocusedWindow")
	if err != nil {
		return "", err
	}
	if len(variant) == 0 || string(variant) == "null" {
		return "", nil
	}
	var window struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(variant, &window); err != nil {
		return "", err
	}
	if window.ID == 0 {
		return "", nil
	}
	return fmt.Sprintf("%d", window.ID), nil
}

func (n *Niri) FocusWindow(id string) error {
	v, err := parseUint64ID(id)
	if err != nil {
		return err
	}
	return n.requestAction(map[string]interface{}{"FocusWindow": map[string]interface{}{"id": v}})
}

func (n *Niri) FocusDir(direction string) error {
	action, err := niriFocusDirection(direction)
	if err != nil {
		return err
	}
	return n.requestAction(action)
}

func niriFocusDirection(direction string) (string, error) {
	switch direction {
	case "l":
		return "FocusColumnLeft", nil
	case "r":
		return "FocusColumnRight", nil
	case "u":
		return "FocusWindowUp", nil
	case "d":
		return "FocusWindowDown", nil
	default:
		return "", fmt.Errorf("invalid direction %q", direction)
	}
}

func (n *Niri) CloseWindow(id string) error {
	args := map[string]interface{}{}
	idArg, err := n.windowIDField(id)
	if err != nil {
		return err
	}
	if idArg != nil {
		args["id"] = idArg["id"]
	}
	return n.requestAction(map[string]interface{}{"CloseWindow": args})
}

func (n *Niri) MoveWindow(id string, direction string) error {
	action, err := niriMoveDirection(direction)
	if err != nil {
		return err
	}
	if id != "" {
		if err := n.FocusWindow(id); err != nil {
			return err
		}
	}
	return n.requestAction(action)
}

func niriMoveDirection(direction string) (string, error) {
	switch direction {
	case "l":
		return "MoveColumnLeft", nil
	case "r":
		return "MoveColumnRight", nil
	case "u":
		return "MoveWindowUp", nil
	case "d":
		return "MoveWindowDown", nil
	default:
		return "", fmt.Errorf("invalid direction %q", direction)
	}
}

func (n *Niri) ResizeWindow(id string, width, height int) error {
	idArg, err := n.windowIDField(id)
	if err != nil {
		return err
	}

	widthAction := map[string]interface{}{
		"change": map[string]interface{}{"SetFixed": width},
	}
	if idArg != nil {
		widthAction["id"] = idArg["id"]
	}
	if err := n.requestAction(map[string]interface{}{"SetWindowWidth": widthAction}); err != nil {
		return err
	}

	heightAction := map[string]interface{}{
		"change": map[string]interface{}{"SetFixed": height},
	}
	if idArg != nil {
		heightAction["id"] = idArg["id"]
	}
	return n.requestAction(map[string]interface{}{"SetWindowHeight": heightAction})
}

func (n *Niri) ToggleFloating(id string) error {
	args := map[string]interface{}{}
	idArg, err := n.windowIDField(id)
	if err != nil {
		return err
	}
	if idArg != nil {
		args["id"] = idArg["id"]
	}
	return n.requestAction(map[string]interface{}{"ToggleWindowFloating": args})
}

func (n *Niri) SetFullscreen(id string, state bool) error {
	return ipc.ErrNotSupported
}

func (n *Niri) SetMaximized(id string, state bool) error {
	return ipc.ErrNotSupported
}

func (n *Niri) PinWindow(id string, state bool) error {
	return ipc.ErrNotSupported
}

func (n *Niri) ToggleGroup(id string) error {
	return ipc.ErrNotSupported
}

func (n *Niri) GroupNav(direction string) error {
	return ipc.ErrNotSupported
}

func (n *Niri) SetLayoutProperty(id string, key, value string) error {
	return ipc.ErrNotSupported
}

func (n *Niri) ListWorkspaces() ([]ipc.Workspace, error) {
	raw, err := n.requestRaw("Workspaces")
	if err != nil {
		return nil, err
	}
	variant, err := unwrapVariant(raw, "Workspaces")
	if err != nil {
		return nil, err
	}
	var niriWorkspaces []niriWorkspace
	if err := json.Unmarshal(variant, &niriWorkspaces); err != nil {
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
		activeWindowID := ""
		if w.ActiveWindowID != nil {
			activeWindowID = fmt.Sprintf("%d", *w.ActiveWindowID)
		}
		res[i] = ipc.Workspace{
			ID:        fmt.Sprintf("%d", w.ID),
			Name:      name,
			MonitorID: output,
			IsActive:  w.IsActive,
			IsEmpty:   false,
			Metadata: map[string]interface{}{
				"focused":          w.IsFocused,
				"index":            w.Idx,
				"active_window_id": activeWindowID,
				"is_urgent":        w.IsUrgent,
			},
		}
	}
	return res, nil
}

func (n *Niri) ActiveWorkspace() (*ipc.Workspace, error) {
	workspaces, err := n.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	for i := range workspaces {
		if v, ok := workspaces[i].Metadata["focused"].(bool); ok && v {
			return &workspaces[i], nil
		}
	}
	return nil, fmt.Errorf("no focused workspace found")
}

func (n *Niri) SwitchWorkspace(id string) error {
	ref, err := n.workspaceReference(id)
	if err != nil {
		return err
	}
	return n.requestAction(map[string]interface{}{
		"FocusWorkspace": map[string]interface{}{
			"reference": ref["reference"],
		},
	})
}

func (n *Niri) MoveToWorkspace(windowID, workspaceID string) error {
	return n.moveWindowToWorkspace(windowID, workspaceID, true)
}

func (n *Niri) MoveToWorkspaceSilent(windowID, workspaceID string) error {
	return n.moveWindowToWorkspace(windowID, workspaceID, false)
}

func (n *Niri) moveWindowToWorkspace(windowID, workspaceID string, focus bool) error {
	ref, err := n.workspaceReference(workspaceID)
	if err != nil {
		return err
	}

	args := map[string]interface{}{
		"reference": ref["reference"],
		"focus":     focus,
	}
	if windowID != "" {
		idArg, err := n.windowIDField(windowID)
		if err != nil {
			return err
		}
		args["window_id"] = idArg["id"]
	}

	return n.requestAction(map[string]interface{}{"MoveWindowToWorkspace": args})
}

func (n *Niri) ListMonitors() ([]ipc.Monitor, error) {
	raw, err := n.requestRaw("Outputs")
	if err != nil {
		return nil, err
	}
	variant, err := unwrapVariant(raw, "Outputs")
	if err != nil {
		return nil, err
	}
	var outputs map[string]niriOutput
	if err := json.Unmarshal(variant, &outputs); err != nil {
		return nil, err
	}

	res := make([]ipc.Monitor, 0, len(outputs))
	for name, o := range outputs {
		m := ipc.Monitor{
			ID:          name,
			Name:        name,
			Description: fmt.Sprintf("%s %s", o.Make, o.Model),
			IsFocused:   false,
			Metadata:    make(map[string]interface{}),
		}
		m.Metadata["make"] = o.Make
		m.Metadata["model"] = o.Model
		m.Metadata["serial"] = o.Serial
		m.Metadata["is_custom_mode"] = o.IsCustomMode
		m.Metadata["vrr_supported"] = o.VRRSupported
		m.Metadata["vrr_enabled"] = o.VRREnabled
		m.Metadata["max_bpc"] = o.MaxBPC
		if o.PhysicalSize != nil {
			m.Metadata["physical_size"] = map[string]interface{}{
				"width":  o.PhysicalSize[0],
				"height": o.PhysicalSize[1],
			}
		}
		if o.Logical != nil {
			m.Width = int(o.Logical.Width)
			m.Height = int(o.Logical.Height)
			m.Scale = o.Logical.Scale
			m.Metadata["x"] = o.Logical.X
			m.Metadata["y"] = o.Logical.Y
			m.Metadata["transform"] = niriTransformToInt(o.Logical.Transform)
		}
		if o.CurrentMode != nil && int(*o.CurrentMode) < len(o.Modes) {
			mode := o.Modes[*o.CurrentMode]
			m.RefreshRate = float64(mode.RefreshRate) / 1000.0
			if m.Width == 0 {
				m.Width = int(mode.Width)
			}
			if m.Height == 0 {
				m.Height = int(mode.Height)
			}
		}
		res = append(res, m)
	}
	return res, nil
}

func niriTransformToInt(t string) int {
	switch t {
	case "Normal":
		return 0
	case "90":
		return 1
	case "180":
		return 2
	case "270":
		return 3
	case "Flipped":
		return 4
	case "Flipped90":
		return 5
	case "Flipped180":
		return 6
	case "Flipped270":
		return 7
	default:
		return 0
	}
}

func (n *Niri) FocusMonitor(id string) error {
	return n.requestAction(map[string]interface{}{"FocusMonitor": map[string]interface{}{"output": id}})
}

func (n *Niri) MoveToMonitor(windowID, monitorID string) error {
	args := map[string]interface{}{"output": monitorID}
	if windowID != "" {
		idArg, err := n.windowIDField(windowID)
		if err != nil {
			return err
		}
		args["id"] = idArg["id"]
	}
	return n.requestAction(map[string]interface{}{"MoveWindowToMonitor": args})
}

func (n *Niri) MoveWindowPixel(id string, x, y int) error {
	args := map[string]interface{}{
		"x": map[string]interface{}{"SetFixed": float64(x)},
		"y": map[string]interface{}{"SetFixed": float64(y)},
	}
	if id != "" {
		idArg, err := n.windowIDField(id)
		if err != nil {
			return err
		}
		args["id"] = idArg["id"]
	}
	return n.requestAction(map[string]interface{}{"MoveFloatingWindow": args})
}

func (n *Niri) ToggleSpecialWorkspace(name string) error {
	return ipc.ErrNotSupported
}

func (n *Niri) GetConfig(key string) (interface{}, error) {
	return nil, ipc.ErrNotSupported
}

func (n *Niri) BatchConfig(configs map[string]interface{}) error {
	for k, v := range configs {
		if err := n.SetConfig(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (n *Niri) BatchKeybinds(jsonPayload string) error {
	return ipc.ErrNotSupported
}

func (n *Niri) RawBatch(command string) error {
	return ipc.ErrNotSupported
}

func (n *Niri) GetAnimations() (interface{}, error) {
	return nil, ipc.ErrNotSupported
}

func (n *Niri) GetCursorPosition() (int, int, error) {
	return 0, 0, ipc.ErrNotSupported
}

func (n *Niri) BindKey(mods, key, command string) error {
	return ipc.ErrNotSupported
}

func (n *Niri) UnbindKey(mods, key string) error {
	return ipc.ErrNotSupported
}

func (n *Niri) SetLayout(name string) error {
	return ipc.ErrNotSupported
}

func (n *Niri) SetConfig(key string, value interface{}) error {
	switch key {
	case "border.active_color", "border.inactive_color":
		_ = ipc.FirstColor(fmt.Sprintf("%v", value))
		return ipc.ErrNotSupported
	default:
		return ipc.ErrNotSupported
	}
}

func (n *Niri) LoadConfig(path string) error {
	args := map[string]interface{}{}
	if path != "" {
		args["path"] = path
	}
	return n.requestAction(map[string]interface{}{"LoadConfigFile": args})
}

func (n *Niri) ReloadConfig() error {
	return n.LoadConfig("")
}

func (n *Niri) SetDpms(monitorID string, on bool) error {
	action := "Off"
	if on {
		action = "On"
	}
	return n.request(map[string]interface{}{
		"Output": map[string]interface{}{
			"output": monitorID,
			"action": action,
		},
	}, nil)
}

func (n *Niri) Execute(command string) error {
	return n.requestAction(map[string]interface{}{
		"Spawn": map[string]interface{}{"command": []string{"sh", "-c", command}},
	})
}

func (n *Niri) Exit() error {
	return n.requestAction(map[string]interface{}{
		"Quit": map[string]interface{}{"skip_confirmation": true},
	})
}

func (n *Niri) Subscribe() (<-chan ipc.Event, error) {
	conn, err := n.dial()
	if err != nil {
		return nil, err
	}

	if err := n.writeRequest(conn, "EventStream"); err != nil {
		_ = conn.Close()
		return nil, err
	}

	dec := json.NewDecoder(conn)
	if _, err := readReplyFromDecoder(dec); err != nil {
		_ = conn.Close()
		return nil, err
	}

	ch := make(chan ipc.Event, 64)
	go func() {
		defer conn.Close()
		defer close(ch)
		for {
			raw, derr := decodeEvent(dec)
			if derr != nil {
				return
			}
			if len(raw) == 0 {
				continue
			}

			event := ipc.Event{
				Timestamp: time.Now().Unix(),
				Payload:   make(map[string]interface{}),
			}

			for name, data := range raw {
				n.handleEvent(name, data, &event)
			}

			if event.Type != "" {
				select {
				case ch <- event:
				default:
				}
			}
		}
	}()

	return ch, nil
}

func decodeEvent(dec *json.Decoder) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (n *Niri) handleEvent(name string, data json.RawMessage, event *ipc.Event) {
	switch name {
	case "WorkspacesChanged":
		event.Type = ipc.EventWorkspaceChanged
	case "WorkspaceUrgencyChanged":
		var d struct {
			ID     uint64 `json:"id"`
			Urgent bool   `json:"urgent"`
		}
		_ = json.Unmarshal(data, &d)
		event.Payload["id"] = fmt.Sprintf("%d", d.ID)
		event.Payload["urgent"] = d.Urgent
	case "WorkspaceActivated":
		event.Type = ipc.EventWorkspaceChanged
		var d struct {
			ID      uint64 `json:"id"`
			Focused bool   `json:"focused"`
		}
		_ = json.Unmarshal(data, &d)
		event.Payload["id"] = fmt.Sprintf("%d", d.ID)
		event.Payload["focused"] = d.Focused
	case "WorkspaceActiveWindowChanged":
		var d struct {
			WorkspaceID    uint64  `json:"workspace_id"`
			ActiveWindowID *uint64 `json:"active_window_id"`
		}
		_ = json.Unmarshal(data, &d)
		event.Payload["workspace_id"] = fmt.Sprintf("%d", d.WorkspaceID)
		if d.ActiveWindowID != nil {
			event.Payload["active_window_id"] = fmt.Sprintf("%d", *d.ActiveWindowID)
		} else {
			event.Payload["active_window_id"] = nil
		}
	case "WindowsChanged":
		event.Type = ipc.EventWorkspaceChanged
	case "WindowOpenedOrChanged":
		var d niriWindowEvent
		_ = json.Unmarshal(data, &d)
		title := ""
		if d.Window.Title != nil {
			title = *d.Window.Title
		}
		appID := ""
		if d.Window.AppID != nil {
			appID = *d.Window.AppID
		}
		event.Window = &ipc.Window{
			ID:         fmt.Sprintf("%d", d.Window.ID),
			Title:      title,
			AppID:      appID,
			IsFocused:  d.Window.IsFocused,
			IsFloating: d.Window.IsFloating,
		}
		if d.Window.IsFocused {
			event.Type = ipc.EventWindowFocused
		} else {
			event.Type = ipc.EventWindowTitleChanged
		}
		event.Payload["id"] = fmt.Sprintf("%d", d.Window.ID)
		event.Payload["title"] = title
	case "WindowClosed":
		event.Type = ipc.EventWindowClosed
		var d struct {
			ID uint64 `json:"id"`
		}
		_ = json.Unmarshal(data, &d)
		event.Payload["id"] = fmt.Sprintf("%d", d.ID)
	case "WindowFocusChanged":
		event.Type = ipc.EventWindowFocused
		var d struct {
			ID *uint64 `json:"id"`
		}
		_ = json.Unmarshal(data, &d)
		if d.ID != nil {
			event.Payload["id"] = fmt.Sprintf("%d", *d.ID)
		} else {
			event.Payload["id"] = nil
		}
	case "WindowLayoutsChanged":
		var d struct {
			Changes [][2]json.RawMessage `json:"changes"`
		}
		_ = json.Unmarshal(data, &d)
		if len(d.Changes) > 0 {
			var id uint64
			_ = json.Unmarshal(d.Changes[0][0], &id)
			event.Payload["id"] = fmt.Sprintf("%d", id)
		}
	case "KeyboardLayoutsChanged", "KeyboardLayoutSwitched":
		event.Type = ipc.EventConfigReloaded
	case "OverviewOpenedOrClosed":
		var d struct {
			IsOpen bool `json:"is_open"`
		}
		_ = json.Unmarshal(data, &d)
		event.Payload["is_open"] = d.IsOpen
	case "ConfigLoaded":
		event.Type = ipc.EventConfigReloaded
		var d struct {
			Failed bool `json:"failed"`
		}
		_ = json.Unmarshal(data, &d)
		event.Payload["failed"] = d.Failed
	}
}

func (n *Niri) SwitchKeyboardLayout(action string) error {
	var target interface{}
	switch action {
	case "next":
		target = "Next"
	case "prev":
		target = "Prev"
	default:
		var idx int
		if _, err := fmt.Sscanf(action, "%d", &idx); err != nil {
			return fmt.Errorf("invalid layout action %q: must be next, prev or an index", action)
		}
		if idx < 0 || idx > 255 {
			return fmt.Errorf("layout index out of range: %d", idx)
		}
		target = idx
	}
	return n.requestAction(map[string]interface{}{"SwitchLayout": target})
}

func (n *Niri) SetKeyboardLayouts(layouts string, variants string) error {
	return ipc.ErrNotSupported
}

func (n *Niri) GetCapabilities() (ipc.Capabilities, error) {
	return ipc.Capabilities{
		Blur:                true,
		Shadows:             true,
		Animations:          true,
		RoundedCorners:      true,
		WorkspacesSupported: true,
		WindowsSupported:    true,
	}, nil
}

type niriWindow struct {
	ID          uint64  `json:"id"`
	Title       *string `json:"title"`
	AppID       *string `json:"app_id"`
	PID         *int32  `json:"pid"`
	WorkspaceID *uint64 `json:"workspace_id"`
	IsFocused   bool    `json:"is_focused"`
	IsFloating  bool    `json:"is_floating"`
	IsUrgent    bool    `json:"is_urgent"`
}

type niriWindowEvent struct {
	Window niriWindow `json:"window"`
}

type niriWorkspace struct {
	ID             uint64  `json:"id"`
	Idx            uint8   `json:"idx"`
	Name           *string `json:"name"`
	Output         *string `json:"output"`
	IsUrgent       bool    `json:"is_urgent"`
	IsActive       bool    `json:"is_active"`
	IsFocused      bool    `json:"is_focused"`
	ActiveWindowID *uint64 `json:"active_window_id"`
}

type niriOutputMode struct {
	Width       uint16 `json:"width"`
	Height      uint16 `json:"height"`
	RefreshRate uint32 `json:"refresh_rate"`
	IsPreferred bool   `json:"is_preferred"`
}

type niriLogicalOutput struct {
	X         int     `json:"x"`
	Y         int     `json:"y"`
	Width     uint32  `json:"width"`
	Height    uint32  `json:"height"`
	Scale     float64 `json:"scale"`
	Transform string  `json:"transform"`
}

type niriOutput struct {
	Name         string             `json:"name"`
	Make         string             `json:"make"`
	Model        string             `json:"model"`
	Serial       *string            `json:"serial"`
	PhysicalSize *[2]uint32         `json:"physical_size"`
	Modes        []niriOutputMode   `json:"modes"`
	CurrentMode  *uint64            `json:"current_mode"`
	IsCustomMode bool               `json:"is_custom_mode"`
	VRRSupported bool               `json:"vrr_supported"`
	VRREnabled   bool               `json:"vrr_enabled"`
	Logical      *niriLogicalOutput `json:"logical"`
	MaxBPC       *uint8             `json:"max_bpc"`
}
