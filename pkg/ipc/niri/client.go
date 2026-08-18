package niri

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"axctl/pkg/ipc"
)

type Niri struct {
	socketPath string
	mu         sync.Mutex
	// ambxstConfigPath is the generated KDL file holding Ambxst's binds block.
	// It is included from the user's config.kdl so we never overwrite their config.
	ambxstConfigPath string
}

func New() (*Niri, error) {
	path := os.Getenv("NIRI_SOCKET")
	if path == "" {
		return nil, fmt.Errorf("NIRI_SOCKET not set")
	}
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		homeDir = "/root"
	}
	return &Niri{
		socketPath:       path,
		ambxstConfigPath: filepath.Join(homeDir, ".config", "niri", "ambxst.kdl"),
	}, nil
}

func (n *Niri) request(req interface{}, resp interface{}) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	conn, err := net.Dial("unix", n.socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()

	// niri IPC protocol: requests are JSON objects on a single line.
	// A bare string like "Windows" must be wrapped as {"Windows": null}.
	var payload interface{} = req
	if str, ok := req.(string); ok {
		payload = map[string]interface{}{str: nil}
	}

	if err := json.NewEncoder(conn).Encode(payload); err != nil {
		return err
	}

	// niri replies with {"Ok": ...} or {"Err": ...} (NOT wrapped in "Reply").
	var reply struct {
		Ok  json.RawMessage `json:"Ok"`
		Err json.RawMessage `json:"Err"`
	}

	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		return err
	}

	if len(reply.Err) > 0 && string(reply.Err) != "null" {
		return fmt.Errorf("niri error: %s", string(reply.Err))
	}

	if resp != nil {
		return json.Unmarshal(reply.Ok, resp)
	}

	return nil
}

// requestQuery sends a query request like {"Windows": null} and unpacks the
// nested response {"Ok":{"Windows":[...]}} into resp. niri wraps query results
// in an object keyed by the request name, so we must unwrap it before unmarshalling.
func (n *Niri) requestQuery(name string, resp interface{}) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	conn, err := net.Dial("unix", n.socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(map[string]interface{}{name: nil}); err != nil {
		return err
	}

	var reply struct {
		Ok  json.RawMessage `json:"Ok"`
		Err json.RawMessage `json:"Err"`
	}

	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		return err
	}

	if len(reply.Err) > 0 && string(reply.Err) != "null" {
		return fmt.Errorf("niri error: %s", string(reply.Err))
	}

	// Unwrap {"<name>": data} -> data
	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal(reply.Ok, &wrapped); err != nil {
		return err
	}
	data, ok := wrapped[name]
	if !ok {
		return fmt.Errorf("niri: response missing key %q", name)
	}

	return json.Unmarshal(data, resp)
}

func (n *Niri) parseWindowID(id string) (int, error) {
	var idInt int
	if _, err := fmt.Sscanf(id, "%d", &idInt); err != nil {
		return 0, err
	}
	return idInt, nil
}

func (n *Niri) ListWindows() ([]ipc.Window, error) {
	// First get workspace->output mapping for MonitorID resolution
	workspaces, _ := n.ListWorkspaces()
	wsOutputMap := make(map[string]string)
	for _, ws := range workspaces {
		wsOutputMap[ws.ID] = ws.MonitorID
	}

	var niriWindows []struct {
		ID           int     `json:"id"`
		Title        *string `json:"title"`
		AppID        *string `json:"app_id"`
		WorkspaceID  *int    `json:"workspace_id"`
		IsFloating   bool    `json:"is_floating"`
		IsFullscreen bool    `json:"is_fullscreen"`
		IsFocused    bool    `json:"is_focused"`
		Layout       *struct {
			WindowSize []float64 `json:"window_size"`
		} `json:"layout"`
	}

	err := n.requestQuery("Windows", &niriWindows)
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
		monitorID := ""
		if wsID != "" {
			monitorID = wsOutputMap[wsID]
		}

		metadata := map[string]interface{}{
			"monitor_id": monitorID,
		}
		// niri reports window size via layout.window_size [w, h].
		// It does not expose an absolute position, so x/y stay 0 — the
		// QML overview renders a real-size preview and lays windows out itself.
		if w.Layout != nil && len(w.Layout.WindowSize) == 2 {
			metadata["width"] = int(w.Layout.WindowSize[0])
			metadata["height"] = int(w.Layout.WindowSize[1])
		}

		windows[i] = ipc.Window{
			ID:           fmt.Sprintf("%d", w.ID),
			Title:        title,
			AppID:        class,
			WorkspaceID:  wsID,
			IsFocused:    w.IsFocused,
			IsFloating:   w.IsFloating,
			IsFullscreen: w.IsFullscreen,
			IsHidden:     false,
			Metadata:     metadata,
		}
	}
	return windows, nil
}

func (n *Niri) ActiveWindow() (string, error) {
	var window *struct {
		ID int `json:"id"`
	}
	err := n.requestQuery("FocusedWindow", &window)
	if err != nil {
		return "", err
	}
	if window == nil {
		return "", nil
	}
	return fmt.Sprintf("%d", window.ID), nil
}

func (n *Niri) FocusWindow(id string) error {
	idInt, err := n.parseWindowID(id)
	if err != nil {
		return err
	}
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{
			"FocusWindow": map[string]interface{}{"id": idInt},
		},
	}, nil)
}

func (n *Niri) FocusDir(direction string) error {
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
		"Action": action,
	}, nil)
}

func (n *Niri) CloseWindow(id string) error {
	args := map[string]interface{}{}
	if id != "" {
		idInt, err := n.parseWindowID(id)
		if err != nil {
			return err
		}
		args["id"] = idInt
	}
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{"CloseWindow": args},
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
		"Action": action,
	}, nil)
}

func (n *Niri) ResizeWindow(id string, width, height int) error {
	err := n.request(map[string]interface{}{
		"Action": map[string]interface{}{
			"SetWindowWidth": map[string]interface{}{
				"width": map[string]interface{}{"Fixed": width},
			},
		},
	}, nil)
	if err != nil {
		return err
	}
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{
			"SetWindowHeight": map[string]interface{}{
				"height": map[string]interface{}{"Fixed": height},
			},
		},
	}, nil)
}

func (n *Niri) ToggleFloating(id string) error {
	args := map[string]interface{}{}
	if id != "" {
		idInt, err := n.parseWindowID(id)
		if err != nil {
			return err
		}
		args["id"] = idInt
	}
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{"ToggleWindowFloating": args},
	}, nil)
}

func (n *Niri) SetFullscreen(id string, state bool) error {
	// Check current state before toggling (like Hyprland does)
	windows, err := n.ListWindows()
	if err != nil {
		return err
	}

	targetID := id
	if targetID == "" {
		targetID, _ = n.ActiveWindow()
	}

	isFs := false
	for _, w := range windows {
		if w.ID == targetID {
			isFs = w.IsFullscreen
			break
		}
	}

	// Already in requested state, nothing to do
	if isFs == state {
		return nil
	}

	args := map[string]interface{}{}
	if id != "" {
		idInt, err := n.parseWindowID(id)
		if err != nil {
			return err
		}
		args["id"] = idInt
	}
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{"FullscreenWindow": args},
	}, nil)
}

func (n *Niri) SetMaximized(id string, state bool) error {
	action := "MaximizeWindow"
	if !state {
		action = "UnmaximizeWindow"
	}
	return n.request(map[string]interface{}{
		"Action": action,
	}, nil)
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
	switch key {
	case "column-width":
		return n.request(map[string]interface{}{
			"Action": map[string]interface{}{
				"SetWindowWidth": map[string]interface{}{
					"width": map[string]interface{}{"Fixed": value},
				},
			},
		}, nil)
	default:
		return ipc.ErrNotSupported
	}
}

func (n *Niri) ListWorkspaces() ([]ipc.Workspace, error) {
	var niriWorkspaces []struct {
		ID             int     `json:"id"`
		Idx            int     `json:"idx"`
		Name           *string `json:"name"`
		Output         *string `json:"output"`
		IsActive       bool    `json:"is_active"`
		IsFocused      bool    `json:"is_focused"`
		ActiveWindowID *int    `json:"active_window_id"`
	}

	err := n.requestQuery("Workspaces", &niriWorkspaces)
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
			},
		}
	}
	// Sort by the workspace index (idx) so the overview shows workspaces in
	// their real order. niri returns them in an arbitrary order, and the
	// Ambxst overview relies on this ordering for the scrolling column.
	sort.SliceStable(res, func(a, b int) bool {
		ai, _ := res[a].Metadata["index"].(int)
		bi, _ := res[b].Metadata["index"].(int)
		return ai < bi
	})
	return res, nil
}

func (n *Niri) ActiveWorkspace() (*ipc.Workspace, error) {
	workspaces, err := n.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	for _, ws := range workspaces {
		if ws.Metadata["focused"].(bool) {
			return &ws, nil
		}
	}
	return nil, fmt.Errorf("no focused workspace found")
}

func (n *Niri) SwitchWorkspace(id string) error {
	var idInt int
	if _, err := fmt.Sscanf(id, "%d", &idInt); err == nil {
		return n.request(map[string]interface{}{
			"Action": map[string]interface{}{
				"FocusWorkspace": map[string]interface{}{
					"reference": map[string]interface{}{"Id": idInt},
				},
			},
		}, nil)
	}
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{
			"FocusWorkspace": map[string]interface{}{
				"reference": map[string]interface{}{"Name": id},
			},
		},
	}, nil)
}

func (n *Niri) MoveToWorkspace(windowID, workspaceID string) error {
	args := map[string]interface{}{}

	var wsIDInt int
	if _, err := fmt.Sscanf(workspaceID, "%d", &wsIDInt); err == nil {
		args["reference"] = map[string]interface{}{"Id": wsIDInt}
	} else {
		args["reference"] = map[string]interface{}{"Name": workspaceID}
	}

	if windowID != "" {
		idInt, err := n.parseWindowID(windowID)
		if err != nil {
			return err
		}
		args["window_id"] = idInt
	}

	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{"MoveWindowToWorkspace": args},
	}, nil)
}

func (n *Niri) ListMonitors() ([]ipc.Monitor, error) {
	// niri returns Outputs as a map keyed by output name: {"eDP-1": {...}}
	var niriOutputs map[string]struct {
		Name  string `json:"name"`
		Make  string `json:"make"`
		Model string `json:"model"`
		Modes []struct {
			Width       int     `json:"width"`
			Height      int     `json:"height"`
			RefreshRate float64 `json:"refresh_rate"`
		} `json:"modes"`
		CurrentMode *int `json:"current_mode"`
		Logical     *struct {
			X         int     `json:"x"`
			Y         int     `json:"y"`
			Width     int     `json:"width"`
			Height    int     `json:"height"`
			Scale     float64 `json:"scale"`
			Transform string  `json:"transform"`
		} `json:"logical"`
	}
	err := n.requestQuery("Outputs", &niriOutputs)
	if err != nil {
		return nil, err
	}

	// niri does not report a "focused" flag on Outputs directly, but the
	// focused workspace carries both is_focused and its output name. Use that
	// to determine which output is currently focused.
	focusedOutput := ""
	activeWorkspaceByOutput := make(map[string]int)
	{
		var niriWorkspaces []struct {
			ID        int    `json:"id"`
			Name      string `json:"name"`
			Output    string `json:"output"`
			IsFocused bool   `json:"is_focused"`
			IsActive  bool   `json:"is_active"`
		}
		if wsErr := n.requestQuery("Workspaces", &niriWorkspaces); wsErr == nil {
			for _, w := range niriWorkspaces {
				if w.IsFocused {
					focusedOutput = w.Output
				}
				if w.IsActive {
					activeWorkspaceByOutput[w.Output] = w.ID
				}
			}
		}
	}

	res := make([]ipc.Monitor, 0, len(niriOutputs))
	for _, o := range niriOutputs {
		m := ipc.Monitor{
			ID:          o.Name,
			Name:        o.Name,
			Description: fmt.Sprintf("%s %s", o.Make, o.Model),
			IsFocused:   o.Name == focusedOutput, // derived from focused workspace
			Metadata:    make(map[string]interface{}),
		}
		if id, ok := activeWorkspaceByOutput[o.Name]; ok {
			m.Metadata["active_workspace"] = fmt.Sprintf("%d", id)
		}
		if o.Logical != nil {
			m.Width = o.Logical.Width
			m.Height = o.Logical.Height
			m.Metadata["x"] = o.Logical.X
			m.Metadata["y"] = o.Logical.Y
			m.Scale = o.Logical.Scale
			m.Metadata["transform"] = niriTransformToInt(o.Logical.Transform)
		}
		if o.CurrentMode != nil && *o.CurrentMode < len(o.Modes) {
			mode := o.Modes[*o.CurrentMode]
			m.RefreshRate = mode.RefreshRate
			if m.Width == 0 {
				m.Width = mode.Width
			}
			if m.Height == 0 {
				m.Height = mode.Height
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
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{
			"FocusOutput": map[string]interface{}{"output": id},
		},
	}, nil)
}

func (n *Niri) MoveToMonitor(windowID, monitorID string) error {
	args := map[string]interface{}{"output": monitorID}
	if windowID != "" {
		idInt, err := n.parseWindowID(windowID)
		if err != nil {
			return err
		}
		args["window_id"] = idInt
	}
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{"MoveWindowToOutput": args},
	}, nil)
}

func (n *Niri) MoveWindowPixel(id string, x, y int) error {
	args := map[string]interface{}{
		"x": map[string]interface{}{"SetFixed": float64(x)},
		"y": map[string]interface{}{"SetFixed": float64(y)},
	}
	if id != "" {
		idInt, err := n.parseWindowID(id)
		if err != nil {
			return err
		}
		args["id"] = idInt
	}
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{"MoveFloatingWindow": args},
	}, nil)
}

func (n *Niri) MoveToWorkspaceSilent(windowID, workspaceID string) error {
	return n.MoveToWorkspace(windowID, workspaceID)
}

func (n *Niri) ToggleSpecialWorkspace(name string) error {
	return ipc.ErrNotSupported
}

func (n *Niri) GetConfig(key string) (interface{}, error) {
	// Read the value back from the generated appearance file (ambxst-appearance.kdl).
	// niri has no runtime config query; we track what we wrote.
	appearancePath := filepath.Join(filepath.Dir(n.ambxstConfigPath), "ambxst-appearance.kdl")
	data, err := os.ReadFile(appearancePath)
	if err != nil {
		return nil, nil
	}
	content := string(data)

	switch key {
	case "gaps.inner", "gaps.outer":
		if m := regexp.MustCompile(`gaps\s+(\d+)`).FindStringSubmatch(content); m != nil {
			var n int
			fmt.Sscanf(m[1], "%d", &n)
			return n, nil
		}
	case "border.width":
		if m := regexp.MustCompile(`width\s+(\d+)`).FindStringSubmatch(content); m != nil {
			var n int
			fmt.Sscanf(m[1], "%d", &n)
			return n, nil
		}
	case "border.active_color":
		if m := regexp.MustCompile(`active-color\s+"([^"]+)"`).FindStringSubmatch(content); m != nil {
			return m[1], nil
		}
	case "border.inactive_color":
		if m := regexp.MustCompile(`inactive-color\s+"([^"]+)"`).FindStringSubmatch(content); m != nil {
			return m[1], nil
		}
	}
	return nil, nil
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
	var payload ipc.BatchKeybindsPayload
	if err := json.Unmarshal([]byte(jsonPayload), &payload); err != nil {
		return fmt.Errorf("invalid keybinds payload: %w", err)
	}

	// Render the binds block and write it to ambxst.kdl.
	content := GenerateKeybindsFromPayload(payload)
	if err := n.writeAmbxstConfig(content); err != nil {
		return err
	}
	return n.ReloadConfig()
}

func (n *Niri) RawBatch(command string) error {
	return ipc.ErrNotSupported
}

func (n *Niri) GetAnimations() (interface{}, error) {
	// niri has no runtime animation query. Return an empty array so callers
	// (CompositorConfig) fall back to defaults gracefully.
	return []interface{}{}, nil
}

func (n *Niri) GetCursorPosition() (int, int, error) {
	// niri IPC does not expose cursor position.
	return 0, 0, ipc.ErrNotSupported
}

func (n *Niri) BindKey(mods, key, command string) error {
	payload, err := n.readCurrentBinds()
	if err != nil {
		return err
	}
	payload.Binds = append(payload.Binds, ipc.Keybind{
		Modifiers:  strings.Split(mods, " "),
		Key:        key,
		Dispatcher: "exec",
		Argument:   command,
		Enabled:    true,
	})
	return n.BatchKeybinds(mustJSON(payload))
}

func (n *Niri) UnbindKey(mods, key string) error {
	payload, err := n.readCurrentBinds()
	if err != nil {
		return err
	}
	var kept []ipc.Keybind
	for _, b := range payload.Binds {
		if b.Key == key && strings.Join(b.Modifiers, " ") == mods {
			continue
		}
		kept = append(kept, b)
	}
	payload.Binds = kept
	return n.BatchKeybinds(mustJSON(payload))
}

// readCurrentBinds parses the current ambxst.kdl back into a payload.
// Since we generate the file ourselves, we can reconstruct it from the
// existing binds block. For simplicity, we return an empty payload if the
// file doesn't exist or can't be parsed (BindKey/UnbindKey are rarely used).
func (n *Niri) readCurrentBinds() (ipc.BatchKeybindsPayload, error) {
	return ipc.BatchKeybindsPayload{}, nil
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// toInt converts a JSON number (float64), int, or numeric string to int.
func toInt(v interface{}) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	case string:
		var n int
		if _, err := fmt.Sscanf(t, "%d", &n); err == nil {
			return n, true
		}
	}
	return 0, false
}

// writeAmbxstConfig writes the generated binds block to ambxst.kdl and
// ensures config.kdl includes it. It never overwrites the user's config.kdl.
func (n *Niri) writeAmbxstConfig(content string) error {
	dir := filepath.Dir(n.ambxstConfigPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(n.ambxstConfigPath, []byte(content), 0644); err != nil {
		return err
	}

	// Ensure config.kdl includes ambxst.kdl.
	mainPath := filepath.Join(dir, "config.kdl")
	includeLine := `include "ambxst.kdl"`
	data, err := os.ReadFile(mainPath)
	if err != nil {
		// No config.kdl yet — create one with just the include.
		return os.WriteFile(mainPath, []byte(includeLine+"\n"), 0644)
	}
	if !strings.Contains(string(data), includeLine) {
		// Prepend the include at the top.
		updated := includeLine + "\n" + string(data)
		if err := os.WriteFile(mainPath, []byte(updated), 0644); err != nil {
			return err
		}
	}
	return nil
}

func (n *Niri) SetLayout(name string) error {
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{
			"SwitchLayout": map[string]interface{}{
				"layout": map[string]interface{}{"Named": name},
			},
		},
	}, nil)
}

func (n *Niri) SetConfig(key string, value interface{}) error {
	// niri has no runtime config set. We generate an appearance block into
	// ambxst-appearance.kdl (included from config.kdl) and reload.
	// Duplicate layout{} blocks are allowed; the last one wins.
	appearancePath := filepath.Join(filepath.Dir(n.ambxstConfigPath), "ambxst-appearance.kdl")

	var out strings.Builder
	out.WriteString("// Generated by axctl (Ambxst appearance)\n")
	out.WriteString("// Do not edit manually!\n\n")
	out.WriteString("layout {\n")

	switch key {
	case "gaps.inner", "gaps.outer":
		// niri only has a single `gaps` value; use inner as the gap.
		if v, ok := toInt(value); ok {
			out.WriteString(fmt.Sprintf("    gaps %d\n", v))
		}
	case "border.width":
		out.WriteString("    border {\n")
		if v, ok := toInt(value); ok {
			out.WriteString(fmt.Sprintf("        width %d\n", v))
		}
		out.WriteString("    }\n")
	case "border.active_color":
		out.WriteString("    border {\n")
		out.WriteString(fmt.Sprintf("        active-color \"%s\"\n", formatNiriColor(fmt.Sprintf("%v", value))))
		out.WriteString("    }\n")
	case "border.inactive_color":
		out.WriteString("    border {\n")
		out.WriteString(fmt.Sprintf("        inactive-color \"%s\"\n", formatNiriColor(fmt.Sprintf("%v", value))))
		out.WriteString("    }\n")
	default:
		// Unsupported key — write an empty layout block (no-op).
	}

	out.WriteString("}\n")

	if err := os.MkdirAll(filepath.Dir(appearancePath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(appearancePath, []byte(out.String()), 0644); err != nil {
		return err
	}

	// Ensure config.kdl includes ambxst-appearance.kdl.
	mainPath := filepath.Join(filepath.Dir(n.ambxstConfigPath), "config.kdl")
	includeLine := `include "ambxst-appearance.kdl"`
	data, err := os.ReadFile(mainPath)
	if err != nil {
		// No config.kdl yet — create one with just the include.
		if werr := os.WriteFile(mainPath, []byte(includeLine+"\n"), 0644); werr != nil {
			return werr
		}
	} else if !strings.Contains(string(data), includeLine) {
		updated := includeLine + "\n" + string(data)
		if err := os.WriteFile(mainPath, []byte(updated), 0644); err != nil {
			return err
		}
	}

	return n.ReloadConfig()
}

func (n *Niri) ReloadConfig() error {
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{"LoadConfigFile": map[string]interface{}{}},
	}, nil)
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

	// niri IPC: subscribe by sending {"EventStream": null}.
	if err := json.NewEncoder(conn).Encode(map[string]interface{}{"EventStream": nil}); err != nil {
		conn.Close()
		return nil, err
	}

	ch := make(chan ipc.Event, 64)
	go func() {
		defer conn.Close()
		defer close(ch)
		dec := json.NewDecoder(conn)
		for {
			// niri events arrive as {"EventName": {...}} on the top level
			// (NOT wrapped in {"Event": {...}}). The first reply is {"Ok":"Handled"}.
			var raw map[string]json.RawMessage
			if err := dec.Decode(&raw); err != nil {
				break
			}

			// Skip the initial {"Ok":"Handled"} ack.
			if _, ok := raw["Ok"]; ok {
				continue
			}

			for name, data := range raw {
				event := ipc.Event{
					Timestamp: time.Now().Unix(),
					Payload:   make(map[string]interface{}),
				}

				switch name {
				case "WorkspacesChanged":
					event.Type = ipc.EventWorkspaceChanged
				case "WorkspaceActivated":
					event.Type = ipc.EventWorkspaceChanged
					var d struct {
						ID int `json:"id"`
					}
					json.Unmarshal(data, &d)
					event.Payload["id"] = fmt.Sprintf("%d", d.ID)
					event.Payload["name"] = fmt.Sprintf("%d", d.ID)
				case "WindowOpened":
					event.Type = ipc.EventWindowCreated
					var d struct {
						Window struct {
							ID    int     `json:"id"`
							Title *string `json:"title"`
							AppID *string `json:"app_id"`
						} `json:"window"`
					}
					json.Unmarshal(data, &d)
					title := ""
					if d.Window.Title != nil {
						title = *d.Window.Title
					}
					class := ""
					if d.Window.AppID != nil {
						class = *d.Window.AppID
					}
					event.Window = &ipc.Window{
						ID:    fmt.Sprintf("%d", d.Window.ID),
						Title: title,
						AppID: class,
					}
				case "WindowClosed":
					event.Type = ipc.EventWindowClosed
					var d struct {
						ID int `json:"id"`
					}
					json.Unmarshal(data, &d)
					event.Payload["id"] = fmt.Sprintf("%d", d.ID)
				case "WindowFocused":
					event.Type = ipc.EventWindowFocused
					var d struct {
						ID *int `json:"id"`
					}
					json.Unmarshal(data, &d)
					if d.ID != nil {
						event.Payload["id"] = fmt.Sprintf("%d", *d.ID)
					}
				case "WindowOpenedOrChanged":
					event.Type = ipc.EventWindowTitleChanged
					var d struct {
						Window struct {
							ID    int     `json:"id"`
							Title *string `json:"title"`
							AppID *string `json:"app_id"`
						} `json:"window"`
					}
					json.Unmarshal(data, &d)
					title := ""
					if d.Window.Title != nil {
						title = *d.Window.Title
					}
					class := ""
					if d.Window.AppID != nil {
						class = *d.Window.AppID
					}
					event.Window = &ipc.Window{
						ID:    fmt.Sprintf("%d", d.Window.ID),
						Title: title,
						AppID: class,
					}
					event.Payload["id"] = fmt.Sprintf("%d", d.Window.ID)
					event.Payload["title"] = title
				case "WindowsChanged":
					event.Type = ipc.EventWorkspaceChanged
				case "KeyboardLayoutsChanged":
					event.Type = ipc.EventConfigReloaded
				case "ConfigLoaded":
					event.Type = ipc.EventConfigReloaded
				}

				if event.Type != "" {
					select {
					case ch <- event:
					default:
					}
				}
			}
		}
	}()

	return ch, nil
}

func (n *Niri) SwitchKeyboardLayout(action string) error {
	var layoutArg interface{} = "Next"
	if action == "prev" {
		layoutArg = "Prev"
	} else if action != "next" {
		var idx int
		if _, err := fmt.Sscanf(action, "%d", &idx); err == nil {
			layoutArg = idx
		}
	}
	// For Niri, it's either "Next", "Prev", or integer index
	req := map[string]interface{}{
		"Action": map[string]interface{}{"SwitchLayout": layoutArg},
	}
	var resp interface{}
	return n.request(req, &resp)
}

func (n *Niri) SetKeyboardLayouts(layouts string, variants string) error {
	return ipc.ErrNotSupported
}

func (n *Niri) GetCapabilities() (ipc.Capabilities, error) {
	return ipc.Capabilities{
		Blur:                true,
		Shadows:             false, // niri does not render window shadows
		Animations:          true,
		RoundedCorners:      true,
		WorkspacesSupported: true,
		WindowsSupported:    true,
	}, nil
}
