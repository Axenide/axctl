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
	"axctl/pkg/ipc/wayland/foreign_toplevel_v1"
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
	cachedWsID string
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

type toplevelInfo struct {
	handle     *foreign_toplevel_v1.ForeignToplevelHandleV1
	title      string
	appId      string
	outputName string
	activated  bool
	maximized  bool
	fullscreen bool
	cachedWsID string
	// pending state written by event handlers, committed on "done"
	pendTitle      string
	pendAppId      string
	pendOutputName string
	pendActivated  bool
	pendMaximized  bool
	pendFullscreen bool
}

type Mangowc struct {
	display *client.Display
	manager *dwlipc.IpcManagerV2
	mu      sync.Mutex

	tagCount uint32
	layouts  []string

	outputs      map[uint32]*outputState
	pending      map[uint32]*outputState
	knownWindows map[string]ipc.Window

	toplevelMgr *foreign_toplevel_v1.ForeignToplevelManagerV1
	toplevels   map[uint32]*toplevelInfo

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
		display:      display,
		outputs:      make(map[uint32]*outputState),
		pending:      make(map[uint32]*outputState),
		knownWindows: make(map[string]ipc.Window),
		toplevels:    make(map[uint32]*toplevelInfo),
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

		case "zwlr_foreign_toplevel_manager_v1":
			tmgr := foreign_toplevel_v1.NewForeignToplevelManagerV1(display.Context())
			ver := e.Version
			if ver > 3 {
				ver = 3
			}
			if err := registry.Bind(e.Name, e.Interface, ver, tmgr); err != nil {
				return
			}
			m.toplevelMgr = tmgr
		}
	})

	// Roundtrip 1: receive all globals (manager + outputs)
	if err := m.roundtrip(); err != nil {
		display.Context().Close()
		return nil, fmt.Errorf("roundtrip 1: %w", err)
	}

	if m.toplevelMgr != nil {
		m.setupToplevelHandlers()
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

	// Roundtrip 4: receive initial foreign-toplevel data (if available)
	if m.toplevelMgr != nil {
		if err := m.roundtrip(); err != nil {
			display.Context().Close()
			return nil, fmt.Errorf("roundtrip 4: %w", err)
		}
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

// normalizeDirection converts short direction codes (l, r, u, d) to the full
// words that MangoWC's parse_direction() expects (left, right, up, down).
func normalizeDirection(dir string) string {
	switch strings.ToLower(dir) {
	case "l", "left":
		return "left"
	case "r", "right":
		return "right"
	case "u", "up":
		return "up"
	case "d", "down":
		return "down"
	default:
		return dir
	}
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
				Window:  &ipc.Window{ID: winID, Title: c.title, AppID: c.appid},
				Payload: map[string]interface{}{"title": c.title, "monitor": monName, "id": winID}})
		}
		if c.appid != oldAppid {
			newWin := ipc.Window{
				ID:           winID,
				Title:        c.title,
				AppID:        c.appid,
				WorkspaceID:  "", // to be populated if needed
				IsFloating:   c.floating,
				IsFullscreen: c.fullscreen,
				Metadata: map[string]interface{}{
					"monitor_id": monName,
					"x":          int(c.x),
					"y":          int(c.y),
					"width":      int(c.width),
					"height":     int(c.height),
				},
			}
			// Track this window; emit WindowCreated if never seen before
			m.mu.Lock()
			if _, exists := m.knownWindows[winID]; !exists {
				m.knownWindows[winID] = newWin
				m.mu.Unlock()
				m.emit(ch, ipc.Event{Type: ipc.EventWindowCreated, Timestamp: now,
					Window:  &newWin,
					Payload: map[string]interface{}{"id": winID, "class": c.appid, "title": c.title, "monitor": monName}})
			} else {
				m.knownWindows[winID] = newWin
				m.mu.Unlock()
			}
			m.emit(ch, ipc.Event{Type: ipc.EventWindowFocused, Timestamp: now,
				Window:  &newWin,
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

func (m *Mangowc) setupToplevelHandlers() {
	m.toplevelMgr.SetToplevelHandler(func(e foreign_toplevel_v1.ForeignToplevelManagerV1ToplevelEvent) {
		handle := e.Toplevel
		hid := handle.ID()
		info := &toplevelInfo{handle: handle}
		m.mu.Lock()
		m.toplevels[hid] = info
		m.mu.Unlock()

		handle.SetTitleHandler(func(te foreign_toplevel_v1.ForeignToplevelHandleV1TitleEvent) {
			m.mu.Lock()
			info.pendTitle = te.Title
			m.mu.Unlock()
		})
		handle.SetAppIdHandler(func(ae foreign_toplevel_v1.ForeignToplevelHandleV1AppIdEvent) {
			m.mu.Lock()
			info.pendAppId = ae.AppId
			m.mu.Unlock()
		})
		handle.SetOutputEnterHandler(func(oe foreign_toplevel_v1.ForeignToplevelHandleV1OutputEnterEvent) {
			if oe.Output == nil {
				return
			}
			m.mu.Lock()
			for _, out := range m.outputs {
				if out.wlOutput != nil && out.wlOutput.ID() == oe.Output.ID() {
					info.pendOutputName = out.name
					break
				}
			}
			m.mu.Unlock()
		})
		handle.SetStateHandler(func(se foreign_toplevel_v1.ForeignToplevelHandleV1StateEvent) {
			m.mu.Lock()
			info.pendActivated = false
			info.pendMaximized = false
			info.pendFullscreen = false
			for _, s := range se.State {
				switch s {
				case foreign_toplevel_v1.ToplevelStateActivated:
					info.pendActivated = true
				case foreign_toplevel_v1.ToplevelStateMaximized:
					info.pendMaximized = true
				case foreign_toplevel_v1.ToplevelStateFullscreen:
					info.pendFullscreen = true
				}
			}
			m.mu.Unlock()
		})
		handle.SetDoneHandler(func(foreign_toplevel_v1.ForeignToplevelHandleV1DoneEvent) {
			m.mu.Lock()
			oldTitle := info.title
			oldAppId := info.appId

			info.title = info.pendTitle
			info.appId = info.pendAppId
			info.outputName = info.pendOutputName
			info.activated = info.pendActivated
			info.maximized = info.pendMaximized
			info.fullscreen = info.pendFullscreen

			ch := m.eventCh
			isNew := oldAppId == "" && info.appId != ""
			winID := fmt.Sprintf("%d", hid)
			m.mu.Unlock()

			if ch == nil {
				return
			}
			now := time.Now().Unix()

			w := ipc.Window{
				ID:           winID,
				Title:        info.title,
				AppID:        info.appId,
				WorkspaceID:  "",
				IsFullscreen: info.fullscreen,
				Metadata: map[string]interface{}{
					"monitor_id": info.outputName,
					"maximized":  info.maximized,
				},
			}

			if isNew {
				m.emit(ch, ipc.Event{Type: ipc.EventWindowCreated, Timestamp: now,
					Window:  &w,
					Payload: map[string]interface{}{"id": winID, "class": info.appId, "title": info.title, "monitor": info.outputName}})
			}
			if info.activated {
				m.emit(ch, ipc.Event{Type: ipc.EventWindowFocused, Timestamp: now,
					Window:  &w,
					Payload: map[string]interface{}{"class": info.appId, "title": info.title, "monitor": info.outputName}})
			}
			if info.title != oldTitle && oldTitle != "" {
				m.emit(ch, ipc.Event{Type: ipc.EventWindowTitleChanged, Timestamp: now,
					Window:  &w,
					Payload: map[string]interface{}{"title": info.title, "monitor": info.outputName, "id": winID}})
			}
		})
		handle.SetClosedHandler(func(foreign_toplevel_v1.ForeignToplevelHandleV1ClosedEvent) {
			m.mu.Lock()
			info := m.toplevels[hid]
			delete(m.toplevels, hid)
			ch := m.eventCh
			m.mu.Unlock()

			if ch == nil || info == nil {
				return
			}
			winID := fmt.Sprintf("%d", hid)
			w := ipc.Window{
				ID:           winID,
				Title:        info.title,
				AppID:        info.appId,
				Metadata: map[string]interface{}{
					"monitor_id": info.outputName,
				},
			}
			m.emit(ch, ipc.Event{Type: ipc.EventWindowClosed, Timestamp: time.Now().Unix(),
				Window:  &w,
				Payload: map[string]interface{}{"id": winID, "class": info.appId, "title": info.title}})
		})
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

	if m.toplevelMgr != nil && len(m.toplevels) > 0 {
		var windows []ipc.Window
		for hid, info := range m.toplevels {
			if info.appId == "" {
				continue
			}
			// Update cached workspace ID only if it doesn't have one yet (newly created)
			// Because of race conditions between wlr-foreign-toplevel and dwl-ipc, we CANNOT trust info.activated 
			// during workspace switches. If we update it here, it might grab the newly switched tag instead of its own!
			if info.cachedWsID == "" {
				for _, out := range m.outputs {
					if out.name == info.outputName {
						for i, t := range out.tags {
							if t.state&uint32(dwlipc.IpcOutputV2TagStateActive) != 0 {
								info.cachedWsID = makeWorkspaceID(out.name, i+1)
								break
							}
						}
						break
					}
				}
			}
			wsID := info.cachedWsID
			windows = append(windows, ipc.Window{
				ID:           fmt.Sprintf("%d", hid),
				Title:        info.title,
				AppID:        info.appId,
				WorkspaceID:  wsID,
				IsFocused:    info.activated,
				IsFullscreen: info.fullscreen,
				Metadata: map[string]interface{}{
					"monitor_id": info.outputName,
					"maximized":  info.maximized,
				},
			})
		}
		return windows, nil
	}

	// Fallback: dwl-ipc only reports the focused window per output
	var windows []ipc.Window
	for _, out := range m.outputs {
		if out.appid == "" {
			continue
		}
		winID := makeWindowID(out.name, out.appid)
		wsID := ""
		for i, t := range out.tags {
			if t.state&uint32(dwlipc.IpcOutputV2TagStateActive) != 0 {
				wsID = makeWorkspaceID(out.name, i+1)
				break
			}
		}
		w := ipc.Window{
			ID:           winID,
			Title:        out.title,
			AppID:        out.appid,
			WorkspaceID:  wsID,
			IsFocused:    out.active,
			IsFloating:   out.floating,
			IsFullscreen: out.fullscreen,
			Metadata: map[string]interface{}{
				"monitor_id": out.name,
				"x":          int(out.x),
				"y":          int(out.y),
				"width":      int(out.width),
				"height":     int(out.height),
			},
		}
		windows = append(windows, w)
		
		// Update cache, but preserve historical WorkspaceID if it's inactive?
		// No, if it's in out.appid, it IS the active window for this output.
		// So we SHOULD update its WorkspaceID to wsID because it's actively focused here!
		m.knownWindows[winID] = w
	}
	for id, kw := range m.knownWindows {
		found := false
		for _, w := range windows {
			if w.ID == id {
				found = true
				break
			}
		}
		if !found {
			windows = append(windows, kw)
		}
	}
	return windows, nil
}

func (m *Mangowc) ActiveWindow() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.toplevelMgr != nil {
		for hid, info := range m.toplevels {
			if info.activated {
				return fmt.Sprintf("%d", hid), nil
			}
		}
	}

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
	return ipcOut.DispatchCmd("focusdir", normalizeDirection(direction), "", "", "", "")
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
	return ipcOut.DispatchCmd("smartmovewin", normalizeDirection(direction), "", "", "", "")
}

func (m *Mangowc) ResizeWindow(id string, width, height int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}
	return ipcOut.DispatchCmd("resizewin", fmt.Sprintf("%d", width), fmt.Sprintf("%d", height), "", "", "")
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
	return ipcOut.DispatchCmd("movewin", fmt.Sprintf("%d", x), fmt.Sprintf("%d", y), "", "", "")
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
				IsActive:  active,
				IsEmpty:   t.clients == 0,
				Metadata: map[string]interface{}{
					"layout":  layoutName,
					"index":   i,
					"urgent":  urgent,
					"focused": active && out.active,
				},
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
				IsActive:  true,
				IsEmpty:   false,
				Metadata: map[string]interface{}{
					"layout":  layoutName,
					"index":   i,
					"focused": true,
				},
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
			ID:          s.name,
			Name:        s.name,
			Description: "",
			IsFocused:   s.active,
			Scale:       s.scale,
			Metadata: map[string]interface{}{
				"active_workspace": wsName,
			},
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
		return ipcOut.DispatchCmd("setoption", "focuscolor", ipc.MangoColor(v), "", "", "")
	case "border.inactive_color":
		return ipcOut.DispatchCmd("setoption", "bordercolor", ipc.MangoColor(v), "", "", "")
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

func (m *Mangowc) BatchKeybinds(jsonPayload string) error {
	return ipc.ErrNotSupported
}

func (m *Mangowc) RawBatch(command string) error {
	return ipc.ErrNotSupported
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

func (m *Mangowc) SwitchKeyboardLayout(action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}

	arg := "0" // Default to next
	if action != "next" && action != "prev" {
		var idx int
		if _, err := fmt.Sscanf(action, "%d", &idx); err == nil {
			arg = fmt.Sprintf("%d", idx+1) // mangowc uses 1-based index
		}
	}
	return ipcOut.DispatchCmd("switch_keyboard_layout", arg, "", "", "", "")
}

func (m *Mangowc) SetKeyboardLayouts(layouts string, variants string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ipcOut := m.activeIpcOutputLocked()
	if ipcOut == nil {
		return ipc.ErrCompositorNotAvailable
	}

	if variants != "" {
		if err := ipcOut.DispatchCmd("setoption", "xkb_rules_variant", variants, "", "", ""); err != nil {
			return err
		}
	} else {
		ipcOut.DispatchCmd("setoption", "xkb_rules_variant", " ", "", "", "")
	}
	return ipcOut.DispatchCmd("setoption", "xkb_rules_layout", layouts, "", "", "")
}

func (m *Mangowc) GetCapabilities() (ipc.Capabilities, error) {
	return ipc.Capabilities{
		Blur:                true,
		Shadows:             true,
		Animations:          true,
		RoundedCorners:      true,
		WorkspacesSupported: true,
		WindowsSupported:    true,
	}, nil
}
