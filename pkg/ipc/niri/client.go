package niri

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"axctl/pkg/ipc"
)

type Niri struct {
	socketPath string
	mu         sync.Mutex

	fsMu        sync.Mutex
	fsSizes     map[uint64][2]int
	fsOutputs   map[string][2]int
	fsOutputsAt time.Time

	wsOutputs   map[string]string
	wsOutputsAt time.Time

	mapsRefreshing atomic.Bool
}

const outputSizeCacheTTL = 5 * time.Second

func New() (*Niri, error) {
	path := os.Getenv("NIRI_SOCKET")
	if path == "" {
		// niri may not export NIRI_SOCKET to processes started outside its
		// session (systemd units, daemons). Discover the running instance's
		// socket by globbing the runtime dir; real sockets are named
		// niri.<wayland-display>.<pid>.sock.
		matches, err := filepath.Glob(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "niri*.sock"))
		if err != nil || len(matches) == 0 {
			return nil, fmt.Errorf("NIRI_SOCKET not set and no niri socket found in XDG_RUNTIME_DIR")
		}
		path = matches[0]
	}
	return &Niri{socketPath: path, fsSizes: make(map[uint64][2]int)}, nil
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
	windows := make([]ipc.Window, 0)
	niriWindows, err := n.rawWindows()
	if err != nil {
		return nil, err
	}
	n.rememberWindowSizes(niriWindows)

	wsOutputMap := make(map[string]string)
	if workspaces, werr := n.rawWorkspaces(); werr == nil {
		for _, ws := range workspaces {
			if ws.Output != nil {
				wsOutputMap[fmt.Sprintf("%d", ws.ID)] = *ws.Output
			}
		}
		n.fsMu.Lock()
		n.wsOutputs = wsOutputMap
		n.wsOutputsAt = time.Now()
		n.fsMu.Unlock()
	}
	sizes := n.outputLogicalSizes()

	for _, w := range niriWindows {
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

		metadata := map[string]interface{}{
			"monitor_id": monitorID,
			"is_urgent":  w.IsUrgent,
			"pid":        w.PID,
		}
		// niri reports window size via layout.window_size [w, h] in logical
		// pixels. There is no absolute position, so x/y stay unset.
		if w.Layout != nil && w.Layout.WindowSize != nil {
			metadata["width"] = w.Layout.WindowSize[0]
			metadata["height"] = w.Layout.WindowSize[1]
		}

		windows = append(windows, ipc.Window{
			ID:           fmt.Sprintf("%d", w.ID),
			Title:        title,
			AppID:        appID,
			WorkspaceID:  wsID,
			IsFocused:    w.IsFocused,
			IsFloating:   w.IsFloating,
			IsFullscreen: niriWindowIsFullscreen(w, wsOutputMap, sizes),
			IsHidden:     false,
			Metadata:     metadata,
		})
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

// SetFullscreen applies the requested fullscreen state. niri's IPC only
// exposes FullscreenWindow as a toggle, so the current state is inferred by
// comparing the window size with the logical size of its output: a
// fullscreen tile always covers the entire output.
func (n *Niri) SetFullscreen(id string, state bool) error {
	targetID := id
	if targetID == "" {
		var err error
		targetID, err = n.ActiveWindow()
		if err != nil {
			return err
		}
		if targetID == "" {
			return nil
		}
	}

	current, err := n.windowIsFullscreen(targetID)
	if err != nil {
		return err
	}
	if current == state {
		return nil
	}

	v, err := parseUint64ID(targetID)
	if err != nil {
		return err
	}
	return n.requestAction(map[string]interface{}{
		"FullscreenWindow": map[string]interface{}{"id": v},
	})
}

func (n *Niri) windowIsFullscreen(id string) (bool, error) {
	target, err := parseUint64ID(id)
	if err != nil {
		return false, err
	}
	windows, err := n.rawWindows()
	if err != nil {
		return false, err
	}
	for _, w := range windows {
		if w.ID != target {
			continue
		}
		wsOutput := make(map[string]string)
		if workspaces, werr := n.rawWorkspaces(); werr == nil {
			for _, ws := range workspaces {
				if ws.Output != nil {
					wsOutput[fmt.Sprintf("%d", ws.ID)] = *ws.Output
				}
			}
		}
		return niriWindowIsFullscreen(w, wsOutput, n.outputLogicalSizes()), nil
	}
	return false, fmt.Errorf("window %s not found", id)
}

func niriWindowIsFullscreen(w niriWindow, wsOutput map[string]string, sizes map[string][2]int) bool {
	if w.Layout == nil || w.Layout.WindowSize == nil {
		return false
	}
	wsID := ""
	if w.WorkspaceID != nil {
		wsID = fmt.Sprintf("%d", *w.WorkspaceID)
	}
	output, ok := wsOutput[wsID]
	if !ok {
		return false
	}
	size, ok := sizes[output]
	if !ok {
		return false
	}
	return (*w.Layout.WindowSize)[0] == size[0] && (*w.Layout.WindowSize)[1] == size[1]
}

func (n *Niri) rememberWindowSizes(windows []niriWindow) {
	n.fsMu.Lock()
	defer n.fsMu.Unlock()
	for _, w := range windows {
		if w.Layout != nil && w.Layout.WindowSize != nil {
			n.fsSizes[w.ID] = *w.Layout.WindowSize
		}
	}
}

func (n *Niri) forgetWindowSize(id uint64) {
	n.fsMu.Lock()
	delete(n.fsSizes, id)
	n.fsMu.Unlock()
}

// detectFullscreenTransition reports whether a window just entered or left
// fullscreen, inferred from its layout size crossing the logical size of any
// output. Only transitions emit events so routine resizes and scrolling do
// not trigger cache refreshes.
func (n *Niri) detectFullscreenTransition(id uint64, size [2]int) bool {
	outputs := n.eventOutputSizes()
	n.fsMu.Lock()
	defer n.fsMu.Unlock()
	prev, known := n.fsSizes[id]
	n.fsSizes[id] = size
	if !known || prev == size || len(outputs) == 0 {
		return false
	}
	return sizeMatchesAnyOutput(prev, outputs) != sizeMatchesAnyOutput(size, outputs)
}

func sizeMatchesAnyOutput(size [2]int, outputs map[string][2]int) bool {
	for _, o := range outputs {
		if size == o {
			return true
		}
	}
	return false
}

func (n *Niri) cachedOutputSizes() map[string][2]int {
	n.fsMu.Lock()
	fresh := n.fsOutputs != nil && time.Since(n.fsOutputsAt) < outputSizeCacheTTL
	outputs := n.fsOutputs
	n.fsMu.Unlock()
	if fresh {
		return outputs
	}
	sizes := n.outputLogicalSizes()
	n.storeOutputSizes(sizes)
	if sizes == nil {
		return outputs
	}
	return sizes
}

func (n *Niri) storeOutputSizes(sizes map[string][2]int) {
	if sizes == nil {
		return
	}
	n.fsMu.Lock()
	n.fsOutputs = sizes
	n.fsOutputsAt = time.Now()
	n.fsMu.Unlock()
}

// eventOutputSizes is the non-blocking variant for event handlers: it only
// reads the cache (kicking off a background refresh when stale) so the
// event stream goroutine never waits on a socket round-trip.
func (n *Niri) eventOutputSizes() map[string][2]int {
	n.fsMu.Lock()
	fresh := n.fsOutputs != nil && time.Since(n.fsOutputsAt) < outputSizeCacheTTL
	outputs := n.fsOutputs
	n.fsMu.Unlock()
	if !fresh {
		n.refreshStateMapsAsync()
	}
	return outputs
}

// eventWorkspaceOutputMap is the non-blocking workspace->output variant of
// workspaceOutputMap for use inside event handlers.
func (n *Niri) eventWorkspaceOutputMap() map[string]string {
	n.fsMu.Lock()
	fresh := n.wsOutputs != nil && time.Since(n.wsOutputsAt) < outputSizeCacheTTL
	outputs := n.wsOutputs
	n.fsMu.Unlock()
	if !fresh {
		n.refreshStateMapsAsync()
	}
	return outputs
}

// refreshStateMapsAsync refreshes the workspace->output map and the output
// logical sizes in the background, deduplicated with a flag so event
// bursts cannot pile up goroutines.
func (n *Niri) refreshStateMapsAsync() {
	if !n.mapsRefreshing.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer n.mapsRefreshing.Store(false)
		workspaces, err := n.rawWorkspaces()
		sizes := n.outputLogicalSizes()
		n.fsMu.Lock()
		if err == nil {
			m := make(map[string]string, len(workspaces))
			for _, ws := range workspaces {
				if ws.Output != nil {
					m[fmt.Sprintf("%d", ws.ID)] = *ws.Output
				}
			}
			n.wsOutputs = m
			n.wsOutputsAt = time.Now()
		}
		if sizes != nil {
			n.fsOutputs = sizes
			n.fsOutputsAt = time.Now()
		}
		n.fsMu.Unlock()
	}()
}

// workspaceOutputMap maps workspace id -> output name, TTL-cached so event
// handlers can enrich windows with monitor metadata without a request per
// event. Stale on failure; refreshed on the next successful call.
func (n *Niri) workspaceOutputMap() map[string]string {
	n.fsMu.Lock()
	fresh := n.wsOutputs != nil && time.Since(n.wsOutputsAt) < outputSizeCacheTTL
	outputs := n.wsOutputs
	n.fsMu.Unlock()
	if fresh {
		return outputs
	}
	workspaces, err := n.rawWorkspaces()
	if err != nil {
		return outputs
	}
	m := make(map[string]string, len(workspaces))
	for _, ws := range workspaces {
		if ws.Output != nil {
			m[fmt.Sprintf("%d", ws.ID)] = *ws.Output
		}
	}
	n.fsMu.Lock()
	n.wsOutputs = m
	n.wsOutputsAt = time.Now()
	n.fsMu.Unlock()
	return m
}

// outputLogicalSizes returns output name -> [width, height] in logical
// pixels. Failures degrade gracefully to an empty map (no window is then
// considered fullscreen).
func (n *Niri) outputLogicalSizes() map[string][2]int {
	raw, err := n.requestRaw("Outputs")
	if err != nil {
		return nil
	}
	variant, err := unwrapVariant(raw, "Outputs")
	if err != nil {
		return nil
	}
	var outputs map[string]niriOutput
	if err := json.Unmarshal(variant, &outputs); err != nil {
		return nil
	}
	sizes := make(map[string][2]int, len(outputs))
	for name, o := range outputs {
		if o.Logical == nil {
			continue
		}
		sizes[name] = [2]int{int(o.Logical.Width), int(o.Logical.Height)}
	}
	return sizes
}

func (n *Niri) rawWindows() ([]niriWindow, error) {
	raw, err := n.requestRaw("Windows")
	if err != nil {
		return nil, err
	}
	variant, err := unwrapVariant(raw, "Windows")
	if err != nil {
		return nil, err
	}
	var windows []niriWindow
	if err := json.Unmarshal(variant, &windows); err != nil {
		return nil, err
	}
	return windows, nil
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

func (n *Niri) rawWorkspaces() ([]niriWorkspace, error) {
	raw, err := n.requestRaw("Workspaces")
	if err != nil {
		return nil, err
	}
	variant, err := unwrapVariant(raw, "Workspaces")
	if err != nil {
		return nil, err
	}
	var workspaces []niriWorkspace
	if err := json.Unmarshal(variant, &workspaces); err != nil {
		return nil, err
	}
	return workspaces, nil
}

func (n *Niri) ListWorkspaces() ([]ipc.Workspace, error) {
	niriWorkspaces, err := n.rawWorkspaces()
	if err != nil {
		return nil, err
	}

	// Sort by workspace index so consumers see the real workspace order;
	// niri returns them in no guaranteed order.
	sort.SliceStable(niriWorkspaces, func(a, b int) bool {
		return niriWorkspaces[a].Idx < niriWorkspaces[b].Idx
	})

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
	// Hyprland-style relative ids (r+1/r-1, and the bare +1/-1 forms)
	// map to niri's relative focus actions, which clamp at the ends
	// just like Hyprland's r-suffixed relatives.
	switch id {
	case "r+1", "+1":
		return n.requestAction(map[string]interface{}{"FocusWorkspaceDown": map[string]interface{}{}})
	case "r-1", "-1":
		return n.requestAction(map[string]interface{}{"FocusWorkspaceUp": map[string]interface{}{}})
	}
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

	// Outputs carry no focused flag; the focused workspace knows its output.
	// Derive the focused output and each output's active workspace from a
	// single workspaces query. Failures degrade gracefully to unfocused.
	focusedOutput := ""
	activeWorkspaceByOutput := make(map[string]string)
	if wsRaw, wsErr := n.requestRaw("Workspaces"); wsErr == nil {
		if wsVariant, wsErr := unwrapVariant(wsRaw, "Workspaces"); wsErr == nil {
			var niriWorkspaces []niriWorkspace
			if json.Unmarshal(wsVariant, &niriWorkspaces) == nil {
				for _, w := range niriWorkspaces {
					output := ""
					if w.Output != nil {
						output = *w.Output
					}
					if w.IsFocused && focusedOutput == "" {
						focusedOutput = output
					}
					if w.IsActive && output != "" {
						if _, exists := activeWorkspaceByOutput[output]; !exists {
							activeWorkspaceByOutput[output] = fmt.Sprintf("%d", w.ID)
						}
					}
				}
			}
		}
	}

	res := make([]ipc.Monitor, 0, len(outputs))
	for name, o := range outputs {
		m := ipc.Monitor{
			ID:          name,
			Name:        name,
			Description: fmt.Sprintf("%s %s", o.Make, o.Model),
			IsFocused:   name == focusedOutput,
			Metadata:    make(map[string]interface{}),
		}
		if id, ok := activeWorkspaceByOutput[name]; ok {
			m.Metadata["active_workspace"] = id
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

func (n *Niri) ToggleOverview() error {
	return n.requestAction(map[string]interface{}{"ToggleOverview": map[string]interface{}{}})
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

func (n *Niri) ListLayouts() ([]ipc.Layout, error) {
	return []ipc.Layout{
		{Name: "scrolling", Current: true, Source: ipc.LayoutSourceStatic},
	}, nil
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
		var d struct {
			Windows []niriWindow `json:"windows"`
		}
		_ = json.Unmarshal(data, &d)
		n.rememberWindowSizes(d.Windows)
	case "WindowOpenedOrChanged":
		var d niriWindowEvent
		_ = json.Unmarshal(data, &d)
		n.rememberWindowSizes([]niriWindow{d.Window})
		title := ""
		if d.Window.Title != nil {
			title = *d.Window.Title
		}
		appID := ""
		if d.Window.AppID != nil {
			appID = *d.Window.AppID
		}
		wsID := ""
		if d.Window.WorkspaceID != nil {
			wsID = fmt.Sprintf("%d", *d.Window.WorkspaceID)
		}
		// The event window carries the full state; enrich it so the
		// server can add it to the cache with the same shape a dump
		// would have. Without the workspace id the QML side filters
		// the window out until the next full refresh.
		wsOutput := n.eventWorkspaceOutputMap()
		metadata := map[string]interface{}{
			"monitor_id": wsOutput[wsID],
			"is_urgent":  d.Window.IsUrgent,
			"pid":        d.Window.PID,
		}
		if d.Window.Layout != nil && d.Window.Layout.WindowSize != nil {
			metadata["width"] = d.Window.Layout.WindowSize[0]
			metadata["height"] = d.Window.Layout.WindowSize[1]
		}
		event.Window = &ipc.Window{
			ID:           fmt.Sprintf("%d", d.Window.ID),
			Title:        title,
			AppID:        appID,
			WorkspaceID:  wsID,
			IsFocused:    d.Window.IsFocused,
			IsFloating:   d.Window.IsFloating,
			IsFullscreen: niriWindowIsFullscreen(d.Window, wsOutput, n.eventOutputSizes()),
			IsHidden:     false,
			Metadata:     metadata,
		}
		if d.Window.IsFocused {
			event.Payload["address"] = event.Window.ID
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
		n.forgetWindowSize(d.ID)
		event.Payload["id"] = fmt.Sprintf("%d", d.ID)
	case "WindowFocusChanged":
		event.Type = ipc.EventWindowFocused
		var d struct {
			ID *uint64 `json:"id"`
		}
		_ = json.Unmarshal(data, &d)
		if d.ID != nil {
			// The server marks cache focus via the "address" payload;
			// niri only provides the numeric id.
			event.Payload["address"] = fmt.Sprintf("%d", *d.ID)
			event.Payload["id"] = fmt.Sprintf("%d", *d.ID)
		} else {
			event.Payload["id"] = nil
		}
	case "WindowLayoutsChanged":
		var d struct {
			Changes [][2]json.RawMessage `json:"changes"`
		}
		_ = json.Unmarshal(data, &d)
		for _, change := range d.Changes {
			var id uint64
			if err := json.Unmarshal(change[0], &id); err != nil {
				continue
			}
			var layout niriWindowLayout
			if err := json.Unmarshal(change[1], &layout); err != nil || layout.WindowSize == nil {
				continue
			}
			if n.detectFullscreenTransition(id, *layout.WindowSize) {
				event.Type = ipc.EventFullscreenChanged
				event.Payload["id"] = fmt.Sprintf("%d", id)
			}
		}
	case "KeyboardLayoutsChanged", "KeyboardLayoutSwitched":
		event.Type = ipc.EventConfigReloaded
	case "OverviewOpenedOrClosed":
		event.Type = ipc.EventOverviewChanged
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
	ID          uint64            `json:"id"`
	Title       *string           `json:"title"`
	AppID       *string           `json:"app_id"`
	PID         *int32            `json:"pid"`
	WorkspaceID *uint64           `json:"workspace_id"`
	IsFocused   bool              `json:"is_focused"`
	IsFloating  bool              `json:"is_floating"`
	IsUrgent    bool              `json:"is_urgent"`
	Layout      *niriWindowLayout `json:"layout"`
}

type niriWindowLayout struct {
	WindowSize *[2]int `json:"window_size"`
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
