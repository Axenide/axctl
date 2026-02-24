package mangowc

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"axctl/pkg/ipc"
	"axctl/pkg/ipc/mangowc/dwlipc"

	"axctl/pkg/ipc/wayland/client"
)

type tagState struct {
	state   uint32
	clients uint32
	focused uint32
}

type outputState struct {
	name       string
	active     bool
	tags       []tagState
	layoutIdx  uint32
	layoutSym  string
	title      string
	appid      string
	fullscreen bool
	floating   bool
	x, y       int32
	width      int32
	height     int32
	scale      float64
	kbLayout   string
	keymode    string
	wlOutput   *client.Output
	ipcOutput  *dwlipc.IpcOutputV2
}

type Mangowc struct {
	display *client.Display
	manager *dwlipc.IpcManagerV2
	mu      sync.Mutex

	tagCount uint32
	layouts  []string

	outputs map[uint32]*outputState
	pending map[uint32]*outputState

	eventCh    chan ipc.Event
	subscribed bool
	protoErr   error
}

func New() (*Mangowc, error) {
	display, err := client.Connect("")
	if err != nil {
		return nil, fmt.Errorf("wayland connect: %w", err)
	}

	m := &Mangowc{
		display: display,
		outputs: make(map[uint32]*outputState),
		pending: make(map[uint32]*outputState),
	}

	// Capture protocol errors from the compositor.
	// Without this, wl_display.error events are silently dropped
	// and the connection reset appears as an opaque read error.
	display.SetErrorHandler(func(e client.DisplayErrorEvent) {
		m.protoErr = fmt.Errorf("wayland protocol error on object %d: code %d: %s",
			e.ObjectId.ID(), e.Code, e.Message)
	})
	display.SetDeleteIdHandler(func(e client.DisplayDeleteIdEvent) {
		if p := display.Context().GetProxy(e.Id); p != nil {
			display.Context().Unregister(p)
		}
	})

	registry, err := display.GetRegistry()
	if err != nil {
		display.Context().Close()
		return nil, fmt.Errorf("get registry: %w", err)
	}

	registry.SetGlobalHandler(func(e client.RegistryGlobalEvent) {
		switch e.Interface {
		case "zdwl_ipc_manager_v2":
			mgr := dwlipc.NewIpcManagerV2(display.Context())
			// Cap version to 2 — our generated bindings only support v2.
			// Binding a higher version causes a protocol error (broken pipe).
			ver := e.Version
			if ver > 2 {
				ver = 2
			}
			if err := registry.Bind(e.Name, e.Interface, ver, mgr); err != nil {
				return
			}
			m.manager = mgr
			mgr.SetTagsHandler(func(te dwlipc.IpcManagerV2TagsEvent) {
				m.tagCount = te.Amount
			})
			mgr.SetLayoutHandler(func(le dwlipc.IpcManagerV2LayoutEvent) {
				m.layouts = append(m.layouts, le.Name)
			})

		case "wl_output":
			output := client.NewOutput(display.Context())
			ver := e.Version
			if ver > 4 {
				ver = 4
			}
			if err := registry.Bind(e.Name, e.Interface, ver, output); err != nil {
				return
			}
			oid := output.ID()
			st := &outputState{wlOutput: output}
			pend := &outputState{wlOutput: output}
			m.outputs[oid] = st
			m.pending[oid] = pend

			output.SetNameHandler(func(ne client.OutputNameEvent) {
				st.name = ne.Name
				pend.name = ne.Name
			})
		}
	})

	// Roundtrip 1: receive all globals (manager + outputs)
	if err := m.roundtrip(); err != nil {
		display.Context().Close()
		return nil, fmt.Errorf("roundtrip 1: %w", err)
	}

	// Roundtrip 2: receive initial events for bound globals (tags, layouts, output names)
	if err := m.roundtrip(); err != nil {
		display.Context().Close()
		return nil, fmt.Errorf("roundtrip 2: %w", err)
	}

	if m.manager == nil {
		display.Context().Close()
		return nil, fmt.Errorf("zdwl_ipc_manager_v2 not available")
	}

	for oid, st := range m.outputs {
		ipcOut, err := m.manager.GetOutput(st.wlOutput)
		if err != nil {
			continue
		}
		st.ipcOutput = ipcOut
		m.pending[oid].ipcOutput = ipcOut

		st.tags = make([]tagState, m.tagCount)
		m.pending[oid].tags = make([]tagState, m.tagCount)

		m.setupOutputHandlers(oid, ipcOut)
	}

	// Roundtrip 3: receive initial state from ipc outputs (tags, title, appid, etc.)
	if err := m.roundtrip(); err != nil {
		display.Context().Close()
		return nil, fmt.Errorf("roundtrip 3: %w", err)
	}

	for oid, st := range m.outputs {
		if st.name == "" {
			st.name = fmt.Sprintf("output-%d", oid)
			m.pending[oid].name = st.name
		}
	}

	return m, nil
}

// makeWindowID creates a composite window ID from monitor name and appid
// to avoid collisions when multiple instances of the same app are on different outputs.
func makeWindowID(monitorName, appid string) string {
	return monitorName + ":" + appid
}

// makeWorkspaceID creates a composite workspace ID from monitor name and tag number.
func makeWorkspaceID(monitorName string, tagNum int) string {
	return monitorName + ":" + strconv.Itoa(tagNum)
}

// parseWorkspaceID extracts the tag number from a workspace ID.
// Accepts both composite "output:N" and plain "N" formats.
func parseWorkspaceID(id string) (int, error) {
	tagStr := id
	if idx := strings.LastIndex(id, ":"); idx >= 0 {
		tagStr = id[idx+1:]
	}
	tagNum, err := strconv.Atoi(tagStr)
	if err != nil {
		return 0, fmt.Errorf("invalid workspace id %q: %w", id, err)
	}
	if tagNum < 1 {
		return 0, fmt.Errorf("workspace id must be >= 1")
	}
	return tagNum, nil
}

func (m *Mangowc) setupOutputHandlers(oid uint32, ipcOut *dwlipc.IpcOutputV2) {
	p := m.pending[oid]

	ipcOut.SetActiveHandler(func(e dwlipc.IpcOutputV2ActiveEvent) {
		p.active = e.Active != 0
	})
	ipcOut.SetTagHandler(func(e dwlipc.IpcOutputV2TagEvent) {
		idx := int(e.Tag)
		for len(p.tags) <= idx {
			p.tags = append(p.tags, tagState{})
		}
		p.tags[idx] = tagState{state: e.State, clients: e.Clients, focused: e.Focused}
	})
	ipcOut.SetLayoutHandler(func(e dwlipc.IpcOutputV2LayoutEvent) { p.layoutIdx = e.Layout })
	ipcOut.SetTitleHandler(func(e dwlipc.IpcOutputV2TitleEvent) { p.title = e.Title })
	ipcOut.SetAppidHandler(func(e dwlipc.IpcOutputV2AppidEvent) { p.appid = e.Appid })
	ipcOut.SetLayoutSymbolHandler(func(e dwlipc.IpcOutputV2LayoutSymbolEvent) {
		p.layoutSym = e.Layout
	})
	ipcOut.SetFullscreenHandler(func(e dwlipc.IpcOutputV2FullscreenEvent) {
		p.fullscreen = e.IsFullscreen != 0
	})
	ipcOut.SetFloatingHandler(func(e dwlipc.IpcOutputV2FloatingEvent) {
		p.floating = e.IsFloating != 0
	})
	ipcOut.SetXHandler(func(e dwlipc.IpcOutputV2XEvent) { p.x = e.X })
	ipcOut.SetYHandler(func(e dwlipc.IpcOutputV2YEvent) { p.y = e.Y })
	ipcOut.SetWidthHandler(func(e dwlipc.IpcOutputV2WidthEvent) { p.width = e.Width })
	ipcOut.SetHeightHandler(func(e dwlipc.IpcOutputV2HeightEvent) { p.height = e.Height })
	ipcOut.SetKbLayoutHandler(func(e dwlipc.IpcOutputV2KbLayoutEvent) { p.kbLayout = e.KbLayout })
	ipcOut.SetKeymodeHandler(func(e dwlipc.IpcOutputV2KeymodeEvent) { p.keymode = e.Keymode })
	ipcOut.SetScalefactorHandler(func(e dwlipc.IpcOutputV2ScalefactorEvent) {
		p.scale = float64(e.Scalefactor) / 100.0
	})
	ipcOut.SetLastLayerHandler(func(dwlipc.IpcOutputV2LastLayerEvent) {})
	ipcOut.SetToggleVisibilityHandler(func(dwlipc.IpcOutputV2ToggleVisibilityEvent) {})

	ipcOut.SetFrameHandler(func(dwlipc.IpcOutputV2FrameEvent) {
		m.mu.Lock()
		c := m.outputs[oid]

		oldActive := c.active
		oldTitle := c.title
		oldAppid := c.appid
		oldFullscreen := c.fullscreen
		oldTags := make([]tagState, len(c.tags))
		copy(oldTags, c.tags)

		c.active = p.active
		c.tags = make([]tagState, len(p.tags))
		copy(c.tags, p.tags)
		c.layoutIdx = p.layoutIdx
		c.layoutSym = p.layoutSym
		c.title = p.title
		c.appid = p.appid
		c.fullscreen = p.fullscreen
		c.floating = p.floating
		c.x = p.x
		c.y = p.y
		c.width = p.width
		c.height = p.height
		c.scale = p.scale
		c.kbLayout = p.kbLayout
		c.keymode = p.keymode

		ch := m.eventCh
		monName := c.name
		winID := makeWindowID(monName, c.appid)
		m.mu.Unlock()

		if ch == nil {
			return
		}
		now := time.Now().Unix()

		tagsChanged := len(oldTags) != len(c.tags)
		if !tagsChanged {
			for i := range oldTags {
				if oldTags[i] != c.tags[i] {
					tagsChanged = true
					break
				}
			}
		}
		if tagsChanged {
			// Include workspace info for incremental cache updates
			activeWsName := ""
			activeWsID := ""
			for i, t := range c.tags {
				if t.state&uint32(dwlipc.IpcOutputV2TagStateActive) != 0 {
					tagNum := i + 1
					activeWsName = strconv.Itoa(tagNum)
					activeWsID = makeWorkspaceID(monName, tagNum)
					break
				}
			}
			m.emit(ch, ipc.Event{Type: ipc.EventWorkspaceChanged, Timestamp: now,
				Workspace: &ipc.Workspace{ID: activeWsID, Name: activeWsName, MonitorID: monName},
				Payload:   map[string]interface{}{"monitor": monName, "name": activeWsName}})
		}
		if c.title != oldTitle {
			m.emit(ch, ipc.Event{Type: ipc.EventWindowTitleChanged, Timestamp: now,
				Window:  &ipc.Window{ID: winID, Title: c.title, Class: c.appid},
				Payload: map[string]interface{}{"title": c.title, "monitor": monName, "id": winID}})
		}
		if c.appid != oldAppid {
			// New focused window (appid changed = different window got focus)
			m.emit(ch, ipc.Event{Type: ipc.EventWindowFocused, Timestamp: now,
				Window:  &ipc.Window{ID: winID, Title: c.title, Class: c.appid, MonitorID: monName},
				Payload: map[string]interface{}{"class": c.appid, "title": c.title, "monitor": monName}})
		}
		if c.fullscreen != oldFullscreen {
			m.emit(ch, ipc.Event{Type: ipc.EventFullscreenChanged, Timestamp: now,
				Payload: map[string]interface{}{"fullscreen": c.fullscreen, "id": winID}})
		}
		if c.active != oldActive {
			m.emit(ch, ipc.Event{Type: ipc.EventFocusedMonitorChanged, Timestamp: now,
				Payload: map[string]interface{}{"monitor": monName, "active": c.active}})
		}
	})
}

func (m *Mangowc) emit(ch chan ipc.Event, e ipc.Event) {
	select {
	case ch <- e:
	default:
	}
}

func (m *Mangowc) roundtrip() error {
	if err := m.display.Roundtrip(); err != nil {
		if m.protoErr != nil {
			return m.protoErr
		}
		return err
	}
	if m.protoErr != nil {
		return m.protoErr
	}
	return nil
}

func (m *Mangowc) activeOutputLocked() *outputState {
	for _, s := range m.outputs {
		if s.active {
			return s
		}
	}
	for _, s := range m.outputs {
		return s
	}
	return nil
}

func (m *Mangowc) activeIpcOutputLocked() *dwlipc.IpcOutputV2 {
	out := m.activeOutputLocked()
	if out == nil {
		return nil
	}
	return out.ipcOutput
}

func (m *Mangowc) ListWindows() ([]ipc.Window, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var windows []ipc.Window
	for _, s := range m.outputs {
		if s.appid == "" && s.title == "" {
			continue
		}
		wsID := ""
		for i, t := range s.tags {
			if t.state&uint32(dwlipc.IpcOutputV2TagStateActive) != 0 {
				wsID = strconv.Itoa(i + 1)
				break
			}
		}
		windows = append(windows, ipc.Window{
			ID:          makeWindowID(s.name, s.appid),
			Title:       s.title,
			Class:       s.appid,
			WorkspaceID: wsID,
			MonitorID:   s.name,
			Floating:    s.floating,
			Fullscreen:  s.fullscreen,
			X:           int(s.x),
			Y:           int(s.y),
			Width:       int(s.width),
			Height:      int(s.height),
		})
	}
	return windows, nil
}

func (m *Mangowc) ActiveWindow() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := m.activeOutputLocked()
	if out == nil {
		return "", nil
	}
	return makeWindowID(out.name, out.appid), nil
}

func (m *Mangowc) FocusWindow(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}
	return ipcOut.DispatchCmd("focuswindow", id, "", "", "", "")
}

func (m *Mangowc) FocusDir(direction string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}
	return ipcOut.DispatchCmd("focusdir", direction, "", "", "", "")
}

func (m *Mangowc) CloseWindow(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}
	return ipcOut.DispatchCmd("killclient", "", "", "", "", "")
}

func (m *Mangowc) MoveWindow(id string, direction string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}
	return ipcOut.DispatchCmd("movewin", direction, "", "", "", "")
}

func (m *Mangowc) ResizeWindow(id string, width, height int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}
	return ipcOut.DispatchCmd("resizewin", fmt.Sprintf("%d,%d", width, height), "", "", "", "")
}

func (m *Mangowc) ToggleFloating(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}
	return ipcOut.DispatchCmd("togglefloating", "", "", "", "", "")
}

func (m *Mangowc) SetFullscreen(id string, state bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := m.activeOutputLocked()
	if out == nil {
		return ipc.ErrCompositorNotAvailable
	}
	// Only toggle if current state differs from requested state
	if out.fullscreen == state {
		return nil
	}
	return out.ipcOutput.DispatchCmd("togglefullscreen", "", "", "", "", "")
}

func (m *Mangowc) SetMaximized(id string, state bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}
	// NOTE: MangoWC does not expose maximized state via zdwl_ipc_v2,
	// so we cannot check current state before toggling.
	return ipcOut.DispatchCmd("togglemaximizescreen", "", "", "", "", "")
}

func (m *Mangowc) PinWindow(id string, state bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}
	// NOTE: MangoWC does not expose pinned/global state via zdwl_ipc_v2,
	// so we cannot check current state before toggling.
	return ipcOut.DispatchCmd("toggleglobal", "", "", "", "", "")
}

func (m *Mangowc) ToggleGroup(id string) error {
	return ipc.ErrNotSupported
}

func (m *Mangowc) GroupNav(direction string) error {
	return ipc.ErrNotSupported
}

func (m *Mangowc) SetLayoutProperty(id string, key, value string) error {
	return ipc.ErrNotSupported
}

func (m *Mangowc) MoveWindowPixel(id string, x, y int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}
	return ipcOut.DispatchCmd("movewin", fmt.Sprintf("%d,%d", x, y), "", "", "", "")
}

func (m *Mangowc) ListWorkspaces() ([]ipc.Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var workspaces []ipc.Workspace
	for _, out := range m.outputs {
		layoutName := out.layoutSym
		if layoutName == "" && int(out.layoutIdx) < len(m.layouts) {
			layoutName = m.layouts[out.layoutIdx]
		}

		for i, t := range out.tags {
			tagNum := i + 1
			wsID := makeWorkspaceID(out.name, tagNum)
			active := t.state&uint32(dwlipc.IpcOutputV2TagStateActive) != 0
			urgent := t.state&uint32(dwlipc.IpcOutputV2TagStateUrgent) != 0

			workspaces = append(workspaces, ipc.Workspace{
				ID:        wsID,
				Name:      strconv.Itoa(tagNum),
				MonitorID: out.name,
				Active:    active,
				Layout:    layoutName,
				Index:     i,
				Urgent:    urgent,
				Focused:   active && out.active,
			})
		}
	}
	return workspaces, nil
}

func (m *Mangowc) ActiveWorkspace() (*ipc.Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := m.activeOutputLocked()
	if out == nil {
		return nil, fmt.Errorf("no active output")
	}

	for i, t := range out.tags {
		if t.state&uint32(dwlipc.IpcOutputV2TagStateActive) != 0 {
			tagNum := i + 1
			wsID := makeWorkspaceID(out.name, tagNum)
			layoutName := out.layoutSym
			if layoutName == "" && int(out.layoutIdx) < len(m.layouts) {
				layoutName = m.layouts[out.layoutIdx]
			}
			return &ipc.Workspace{
				ID:        wsID,
				Name:      strconv.Itoa(tagNum),
				MonitorID: out.name,
				Active:    true,
				Layout:    layoutName,
				Index:     i,
				Focused:   true,
			}, nil
		}
	}
	return nil, ipc.ErrWorkspaceNotFound
}

func (m *Mangowc) SwitchWorkspace(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}

	tagNum, err := parseWorkspaceID(id)
	if err != nil {
		return err
	}
	tagIdx := tagNum - 1
	return ipcOut.SetTags(1<<uint(tagIdx), 0)
}

func (m *Mangowc) MoveToWorkspace(windowID, workspaceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}

	tagNum, err := parseWorkspaceID(workspaceID)
	if err != nil {
		return err
	}
	tagIdx := tagNum - 1
	return ipcOut.SetClientTags(0, 1<<uint(tagIdx))
}

func (m *Mangowc) MoveToWorkspaceSilent(windowID, workspaceID string) error {
	return m.MoveToWorkspace(windowID, workspaceID)
}

func (m *Mangowc) ToggleSpecialWorkspace(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}
	if name != "" {
		return ipcOut.DispatchCmd("toggle_named_scratchpad", name, "", "", "", "")
	}
	return ipcOut.DispatchCmd("toggle_scratchpad", "", "", "", "", "")
}

func (m *Mangowc) ListMonitors() ([]ipc.Monitor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var monitors []ipc.Monitor
	for _, s := range m.outputs {
		wsName := ""
		for i, t := range s.tags {
			if t.state&uint32(dwlipc.IpcOutputV2TagStateActive) != 0 {
				wsName = strconv.Itoa(i + 1)
				break
			}
		}
		monitors = append(monitors, ipc.Monitor{
			ID:        s.name,
			Name:      s.name,
			Active:    s.active,
			Workspace: wsName,
			Scale:     s.scale,
		})
	}
	return monitors, nil
}

func (m *Mangowc) FocusMonitor(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}
	return ipcOut.DispatchCmd("focusmon", id, "", "", "", "")
}

func (m *Mangowc) MoveToMonitor(windowID, monitorID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}
	return ipcOut.DispatchCmd("tagmon", monitorID, "", "", "", "")
}

func (m *Mangowc) SetDpms(monitorID string, on bool) error {
	return ipc.ErrNotSupported
}

func (m *Mangowc) SetLayout(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}

	for i, l := range m.layouts {
		if l == name {
			return ipcOut.SetLayout(uint32(i))
		}
	}
	return fmt.Errorf("layout %q not found", name)
}

func (m *Mangowc) GetConfig(key string) (interface{}, error) {
	return nil, ipc.ErrNotSupported
}

func (m *Mangowc) SetConfig(key string, value interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}

	v := fmt.Sprint(value)

	switch key {
	case "gaps.inner":
		if err := ipcOut.DispatchCmd("setoption", "gappih", v, "", "", ""); err != nil {
			return err
		}
		return ipcOut.DispatchCmd("setoption", "gappiv", v, "", "", "")
	case "gaps.outer":
		if err := ipcOut.DispatchCmd("setoption", "gappoh", v, "", "", ""); err != nil {
			return err
		}
		return ipcOut.DispatchCmd("setoption", "gappov", v, "", "", "")
	case "border.width":
		return ipcOut.DispatchCmd("setoption", "borderpx", v, "", "", "")
	case "border.active_color":
		return ipcOut.DispatchCmd("setoption", "focuscolor", ipc.FirstColor(v), "", "", "")
	case "border.inactive_color":
		return ipcOut.DispatchCmd("setoption", "bordercolor", ipc.FirstColor(v), "", "", "")
	case "opacity.active":
		return ipcOut.DispatchCmd("setoption", "focused_opacity", v, "", "", "")
	case "opacity.inactive":
		return ipcOut.DispatchCmd("setoption", "unfocused_opacity", v, "", "", "")
	case "blur.enabled":
		return ipcOut.DispatchCmd("setoption", "blur", v, "", "", "")
	case "blur.size":
		return ipcOut.DispatchCmd("setoption", "blur_params_radius", v, "", "", "")
	case "blur.passes":
		return ipcOut.DispatchCmd("setoption", "blur_params_num_passes", v, "", "", "")
	case "blur.brightness":
		return ipcOut.DispatchCmd("setoption", "blur_params_brightness", v, "", "", "")
	case "blur.contrast":
		return ipcOut.DispatchCmd("setoption", "blur_params_contrast", v, "", "", "")
	case "blur.saturation":
		return ipcOut.DispatchCmd("setoption", "blur_params_saturation", v, "", "", "")
	case "shadows":
		return ipcOut.DispatchCmd("setoption", "shadows", v, "", "", "")
	case "rounding", "border_radius":
		return ipcOut.DispatchCmd("setoption", "border_radius", v, "", "", "")
	default:
		return ipc.ErrNotSupported
	}
}

func (m *Mangowc) BatchConfig(configs map[string]interface{}) error {
	for k, v := range configs {
		if err := m.SetConfig(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (m *Mangowc) ReloadConfig() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}
	return ipcOut.DispatchCmd("reload_config", "", "", "", "", "")
}

func (m *Mangowc) GetAnimations() (interface{}, error) {
	return nil, ipc.ErrNotSupported
}

func (m *Mangowc) GetCursorPosition() (int, int, error) {
	return 0, 0, ipc.ErrNotSupported
}

func (m *Mangowc) BindKey(mods, key, command string) error {
	return ipc.ErrNotSupported
}

func (m *Mangowc) UnbindKey(mods, key string) error {
	return ipc.ErrNotSupported
}

func (m *Mangowc) Execute(command string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}
	return ipcOut.DispatchCmd("spawn", command, "", "", "", "")
}

func (m *Mangowc) Exit() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}
	return ipcOut.Quit()
}

func (m *Mangowc) Subscribe() (<-chan ipc.Event, error) {
	m.mu.Lock()
	if m.subscribed {
		m.mu.Unlock()
		return nil, fmt.Errorf("already subscribed")
	}
	m.eventCh = make(chan ipc.Event, 64)
	m.subscribed = true
	ch := m.eventCh
	m.mu.Unlock()

	go func() {
		for {
			dispatchFunc := m.display.Context().GetDispatch()
			if err := dispatchFunc(); err != nil {
				m.mu.Lock()
				close(m.eventCh)
				m.subscribed = false
				m.mu.Unlock()
				return
			}
		}
	}()

	return ch, nil
}
