package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"axctl/pkg/ipc/wayland/client"
	"axctl/pkg/ipc/wayland/ext_idle_notify_v1"
	"axctl/pkg/ipc/wayland/idle_inhibit_v1"
)

type IdleManager struct {
	display      *client.Display
	compositor   *client.Compositor
	seat         *client.Seat
	notifier     *ext_idle_notify_v1.ExtIdleNotifierV1
	inhibitorMgr *idle_inhibit_v1.ZwpIdleInhibitManagerV1

	mu   sync.Mutex
	wlMu sync.Mutex // Protects Wayland socket writes

	legacyInhibitorID uint32

	nextMonitorID uint32
	monitors      map[uint32]*idleMonitor

	nextInhibitorID uint32
	inhibitors      map[uint32]*idleInhibitor

	systemInhibitor systemInhibitor

	onIdleMonitorChanged func(id uint32, isIdle bool)
}

type idleMonitor struct {
	id                uint32
	timeoutMs         uint32
	respectInhibitors bool
	enabled           bool
	isIdle            bool
	deleted           bool
	notification      *ext_idle_notify_v1.ExtIdleNotificationV1
}

type idleInhibitor struct {
	id        uint32
	enabled   bool
	deleted   bool
	inhibitor *idle_inhibit_v1.ZwpIdleInhibitorV1
	surface   *client.Surface
}

type systemInhibitor struct {
	enabled bool
	cmd     *exec.Cmd
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewIdleManager() (*IdleManager, error) {
	display, err := client.Connect("")
	if err != nil {
		return nil, fmt.Errorf("wayland connect failed: %v", err)
	}
	registry, err := display.GetRegistry()
	if err != nil {
		return nil, fmt.Errorf("get registry failed: %v", err)
	}

	im := &IdleManager{
		display:    display,
		monitors:   make(map[uint32]*idleMonitor),
		inhibitors: make(map[uint32]*idleInhibitor),
	}

	registry.SetGlobalHandler(func(e client.RegistryGlobalEvent) {
		switch e.Interface {
		case "wl_compositor":
			im.compositor = client.NewCompositor(display.Context())
			registry.Bind(e.Name, e.Interface, 1, im.compositor)
		case "wl_seat":
			im.seat = client.NewSeat(display.Context())
			registry.Bind(e.Name, e.Interface, 1, im.seat)
		case "ext_idle_notifier_v1":
			im.notifier = ext_idle_notify_v1.NewExtIdleNotifierV1(display.Context())
			ver := e.Version
			if ver > 2 {
				ver = 2
			}
			registry.Bind(e.Name, e.Interface, ver, im.notifier)
		case "zwp_idle_inhibit_manager_v1":
			im.inhibitorMgr = idle_inhibit_v1.NewZwpIdleInhibitManagerV1(display.Context())
			registry.Bind(e.Name, e.Interface, 1, im.inhibitorMgr)
		}
	})

	callback, err := display.Sync()
	if err != nil {
		return nil, err
	}
	done := make(chan struct{})
	callback.SetDoneHandler(func(e client.CallbackDoneEvent) { close(done) })

	for {
		if err := display.Context().GetDispatch()(); err != nil {
			return nil, err
		}
		select {
		case <-done:
			goto AfterSync
		default:
		}
	}
AfterSync:
	go func() {
		for {
			dispatchFunc := display.Context().GetDispatch()
			if err := dispatchFunc(); err != nil {
				return
			}
		}
	}()

	return im, nil
}

func (im *IdleManager) SetIdleMonitorCallback(cb func(id uint32, isIdle bool)) {
	im.mu.Lock()
	im.onIdleMonitorChanged = cb
	im.mu.Unlock()
}

func (im *IdleManager) Inhibit(on bool) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	if im.legacyInhibitorID == 0 {
		im.nextInhibitorID++
		im.legacyInhibitorID = im.nextInhibitorID
		im.inhibitors[im.legacyInhibitorID] = &idleInhibitor{id: im.legacyInhibitorID}
	}

	inh := im.inhibitors[im.legacyInhibitorID]
	if inh == nil {
		return fmt.Errorf("idle inhibitor not initialized")
	}

	if inh.enabled == on {
		return nil
	}

	if err := im.setInhibitorEnabledLocked(inh, on); err != nil {
		return err
	}
	inh.enabled = on
	return nil
}

func (im *IdleManager) waitIdleInternal(timeoutMs uint32, inputOnly bool) error {
	if im.notifier == nil || im.seat == nil {
		return fmt.Errorf("idle_notify not supported by compositor")
	}

	ch := make(chan struct{}, 1)

	im.wlMu.Lock()
	var notif *ext_idle_notify_v1.ExtIdleNotificationV1
	var err error
	if inputOnly {
		notif, err = im.notifier.GetInputIdleNotification(timeoutMs, im.seat)
	} else {
		notif, err = im.notifier.GetIdleNotification(timeoutMs, im.seat)
	}

	if err == nil {
		notif.SetIdledHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1IdledEvent) {
			select {
			case ch <- struct{}{}:
			default:
			}
		})
	}
	im.wlMu.Unlock()

	if err != nil {
		return err
	}

	defer func() {
		im.wlMu.Lock()
		notif.Destroy()
		im.wlMu.Unlock()
	}()

	<-ch
	return nil
}

func (im *IdleManager) WaitIdle(timeoutMs uint32) error {
	return im.waitIdleInternal(timeoutMs, false)
}

func (im *IdleManager) waitResumeInternal(timeoutMs uint32, inputOnly bool) error {
	if im.notifier == nil || im.seat == nil {
		return fmt.Errorf("idle_notify not supported by compositor")
	}

	ch := make(chan struct{}, 1)

	im.wlMu.Lock()
	var notif *ext_idle_notify_v1.ExtIdleNotificationV1
	var err error
	if inputOnly {
		notif, err = im.notifier.GetInputIdleNotification(timeoutMs, im.seat)
	} else {
		notif, err = im.notifier.GetIdleNotification(timeoutMs, im.seat)
	}

	if err != nil {
		im.wlMu.Unlock()
		return err
	}

	var hasIdled bool
	notif.SetIdledHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1IdledEvent) {
		im.mu.Lock()
		hasIdled = true
		im.mu.Unlock()
	})

	notif.SetResumedHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1ResumedEvent) {
		im.mu.Lock()
		ready := hasIdled
		im.mu.Unlock()
		if ready {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	})

	callback, err := im.display.Sync()
	if err != nil {
		notif.Destroy()
		im.wlMu.Unlock()
		return err
	}
	done := make(chan struct{})
	callback.SetDoneHandler(func(e client.CallbackDoneEvent) { close(done) })
	im.wlMu.Unlock()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}

	defer func() {
		im.wlMu.Lock()
		notif.Destroy()
		im.wlMu.Unlock()
	}()

	<-ch
	return nil
}

func (im *IdleManager) isIdleInternal(timeoutMs uint32, inputOnly bool) (bool, error) {
	if im.notifier == nil || im.seat == nil {
		return false, fmt.Errorf("idle_notify not supported by compositor")
	}

	isIdle := false
	done := make(chan struct{})

	im.wlMu.Lock()
	var notif *ext_idle_notify_v1.ExtIdleNotificationV1
	var err error
	if inputOnly {
		notif, err = im.notifier.GetInputIdleNotification(timeoutMs, im.seat)
	} else {
		notif, err = im.notifier.GetIdleNotification(timeoutMs, im.seat)
	}
	if err != nil {
		im.wlMu.Unlock()
		return false, err
	}

	// ALWAYS set the handler BEFORE unlocking wlMu, because the background
	// dispatcher might receive the event immediately!
	notif.SetIdledHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1IdledEvent) {
		isIdle = true
	})
	notif.SetResumedHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1ResumedEvent) {
	})

	// To avoid blocking forever if something goes wrong with Wayland sync
	// we will use a timeout fallback. But normally Sync is instant.
	callback, err := im.display.Sync()
	if err != nil {
		notif.Destroy()
		im.wlMu.Unlock()
		return false, err
	}
	callback.SetDoneHandler(func(e client.CallbackDoneEvent) {
		close(done)
	})
	im.wlMu.Unlock()

	// Wait for the sync callback to finish, or timeout if it deadlocks
	// The background dispatch loop handles events. It will close 'done' when Sync finishes.
	// If the socket was closed, the background loop might exit WITHOUT dispatching our callback!
	select {
	case <-done:
		// Normal completion
	case <-time.After(2 * time.Second):
		// Timeout - the compositor is unresponsive or connection died
		return false, fmt.Errorf("timeout waiting for idle sync callback")
	}

	im.wlMu.Lock()
	notif.Destroy()
	im.wlMu.Unlock()

	return isIdle, nil
}

func (im *IdleManager) IsIdle(timeoutMs uint32) (bool, error) {
	return im.isIdleInternal(timeoutMs, false)
}

func (im *IdleManager) IsInhibited() bool {
	im.mu.Lock()
	defer im.mu.Unlock()
	if im.legacyInhibitorID == 0 {
		return false
	}
	inh := im.inhibitors[im.legacyInhibitorID]
	if inh == nil {
		return false
	}
	return inh.enabled
}

func (im *IdleManager) WaitInputIdle(timeoutMs uint32) error {
	return im.waitIdleInternal(timeoutMs, true)
}

func (im *IdleManager) WaitInputResume(timeoutMs uint32) error {
	return im.waitResumeInternal(timeoutMs, true)
}

func (im *IdleManager) IsInputIdle(timeoutMs uint32) (bool, error) {
	return im.isIdleInternal(timeoutMs, true)
}

func (im *IdleManager) WaitResume(timeoutMs uint32) error {
	return im.waitResumeInternal(timeoutMs, false)
}

type IdleMonitorState struct {
	ID                uint32 `json:"id"`
	Enabled           bool   `json:"enabled"`
	TimeoutMs         uint32 `json:"timeout_ms"`
	RespectInhibitors bool   `json:"respect_inhibitors"`
	IsIdle            bool   `json:"is_idle"`
}

type IdleInhibitorState struct {
	ID      uint32 `json:"id"`
	Enabled bool   `json:"enabled"`
}

func (im *IdleManager) CreateIdleMonitor(timeoutMs uint32, respectInhibitors bool, enabled bool) (IdleMonitorState, error) {
	im.mu.Lock()
	im.nextMonitorID++
	id := im.nextMonitorID
	mon := &idleMonitor{
		id:                id,
		timeoutMs:         timeoutMs,
		respectInhibitors: respectInhibitors,
		enabled:           enabled,
	}
	im.monitors[id] = mon
	im.mu.Unlock()

	if err := im.refreshMonitor(mon); err != nil {
		im.mu.Lock()
		delete(im.monitors, id)
		im.mu.Unlock()
		return IdleMonitorState{}, err
	}

	return im.getMonitorState(mon), nil
}

func (im *IdleManager) UpdateIdleMonitor(id uint32, timeoutMs uint32, respectInhibitors bool, enabled bool) (IdleMonitorState, error) {
	im.mu.Lock()
	mon := im.monitors[id]
	if mon == nil || mon.deleted {
		im.mu.Unlock()
		return IdleMonitorState{}, fmt.Errorf("idle monitor not found")
	}
	mon.timeoutMs = timeoutMs
	mon.respectInhibitors = respectInhibitors
	mon.enabled = enabled
	im.mu.Unlock()

	if err := im.refreshMonitor(mon); err != nil {
		return IdleMonitorState{}, err
	}

	return im.getMonitorState(mon), nil
}

func (im *IdleManager) DestroyIdleMonitor(id uint32) error {
	im.mu.Lock()
	mon := im.monitors[id]
	if mon == nil || mon.deleted {
		im.mu.Unlock()
		return fmt.Errorf("idle monitor not found")
	}
	mon.deleted = true
	delete(im.monitors, id)
	notif := mon.notification
	mon.notification = nil
	im.mu.Unlock()

	if notif != nil {
		im.wlMu.Lock()
		notif.Destroy()
		im.wlMu.Unlock()
	}
	return nil
}

func (im *IdleManager) GetIdleMonitor(id uint32) (IdleMonitorState, error) {
	im.mu.Lock()
	mon := im.monitors[id]
	if mon == nil || mon.deleted {
		im.mu.Unlock()
		return IdleMonitorState{}, fmt.Errorf("idle monitor not found")
	}
	state := im.getMonitorStateLocked(mon)
	im.mu.Unlock()
	return state, nil
}

func (im *IdleManager) getMonitorState(mon *idleMonitor) IdleMonitorState {
	im.mu.Lock()
	state := im.getMonitorStateLocked(mon)
	im.mu.Unlock()
	return state
}

func (im *IdleManager) getMonitorStateLocked(mon *idleMonitor) IdleMonitorState {
	return IdleMonitorState{
		ID:                mon.id,
		Enabled:           mon.enabled,
		TimeoutMs:         mon.timeoutMs,
		RespectInhibitors: mon.respectInhibitors,
		IsIdle:            mon.isIdle,
	}
}

func (im *IdleManager) refreshMonitor(mon *idleMonitor) error {
	im.mu.Lock()
	if mon.deleted {
		im.mu.Unlock()
		return fmt.Errorf("idle monitor destroyed")
	}
	oldNotif := mon.notification
	mon.notification = nil
	enabled := mon.enabled
	timeoutMs := mon.timeoutMs
	respectInhibitors := mon.respectInhibitors
	im.mu.Unlock()

	if oldNotif != nil {
		im.wlMu.Lock()
		oldNotif.Destroy()
		im.wlMu.Unlock()
	}

	im.setMonitorIdle(mon, false)

	if !enabled {
		return nil
	}

	if im.notifier == nil || im.seat == nil {
		return fmt.Errorf("idle_notify not supported by compositor")
	}

	im.wlMu.Lock()
	var notif *ext_idle_notify_v1.ExtIdleNotificationV1
	var err error

	if respectInhibitors {
		notif, err = im.notifier.GetIdleNotification(timeoutMs, im.seat)
	} else {
		notif, err = im.notifier.GetInputIdleNotification(timeoutMs, im.seat)
	}
	if err == nil {
		notif.SetIdledHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1IdledEvent) {
			im.setMonitorIdle(mon, true)
		})
		notif.SetResumedHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1ResumedEvent) {
			im.setMonitorIdle(mon, false)
		})
	}
	im.wlMu.Unlock()

	if err != nil {
		return err
	}

	im.mu.Lock()
	if mon.deleted {
		im.mu.Unlock()
		im.wlMu.Lock()
		notif.Destroy()
		im.wlMu.Unlock()
		return fmt.Errorf("idle monitor destroyed")
	}
	mon.notification = notif
	im.mu.Unlock()
	return nil
}

func (im *IdleManager) setMonitorIdle(mon *idleMonitor, isIdle bool) {
	im.mu.Lock()
	if mon.deleted {
		im.mu.Unlock()
		return
	}
	if mon.isIdle == isIdle {
		im.mu.Unlock()
		return
	}
	mon.isIdle = isIdle
	cb := im.onIdleMonitorChanged
	id := mon.id
	im.mu.Unlock()

	if cb != nil {
		cb(id, isIdle)
	}
}

func (im *IdleManager) CreateIdleInhibitor(enabled bool) (IdleInhibitorState, error) {
	im.mu.Lock()
	im.nextInhibitorID++
	id := im.nextInhibitorID
	inh := &idleInhibitor{id: id}
	im.inhibitors[id] = inh
	if enabled {
		if err := im.setInhibitorEnabledLocked(inh, true); err != nil {
			delete(im.inhibitors, id)
			im.mu.Unlock()
			return IdleInhibitorState{}, err
		}
		inh.enabled = true
	}
	state := IdleInhibitorState{ID: id, Enabled: inh.enabled}
	im.mu.Unlock()
	return state, nil
}

func (im *IdleManager) SetIdleInhibitorEnabled(id uint32, enabled bool) (IdleInhibitorState, error) {
	im.mu.Lock()
	inh := im.inhibitors[id]
	if inh == nil || inh.deleted {
		im.mu.Unlock()
		return IdleInhibitorState{}, fmt.Errorf("idle inhibitor not found")
	}
	if inh.enabled == enabled {
		state := IdleInhibitorState{ID: id, Enabled: inh.enabled}
		im.mu.Unlock()
		return state, nil
	}
	if err := im.setInhibitorEnabledLocked(inh, enabled); err != nil {
		im.mu.Unlock()
		return IdleInhibitorState{}, err
	}
	inh.enabled = enabled
	state := IdleInhibitorState{ID: id, Enabled: inh.enabled}
	im.mu.Unlock()
	return state, nil
}

func (im *IdleManager) GetIdleInhibitor(id uint32) (IdleInhibitorState, error) {
	im.mu.Lock()
	inh := im.inhibitors[id]
	if inh == nil || inh.deleted {
		im.mu.Unlock()
		return IdleInhibitorState{}, fmt.Errorf("idle inhibitor not found")
	}
	state := IdleInhibitorState{ID: id, Enabled: inh.enabled}
	im.mu.Unlock()
	return state, nil
}

func (im *IdleManager) DestroyIdleInhibitor(id uint32) error {
	im.mu.Lock()
	inh := im.inhibitors[id]
	if inh == nil || inh.deleted {
		im.mu.Unlock()
		return fmt.Errorf("idle inhibitor not found")
	}
	inh.deleted = true
	delete(im.inhibitors, id)
	if im.legacyInhibitorID == id {
		im.legacyInhibitorID = 0
	}
	if err := im.setInhibitorEnabledLocked(inh, false); err != nil {
		im.mu.Unlock()
		return err
	}
	im.mu.Unlock()
	return nil
}

func (im *IdleManager) setInhibitorEnabledLocked(inh *idleInhibitor, enabled bool) error {
	im.wlMu.Lock()
	defer im.wlMu.Unlock()

	if enabled {
		if inh.inhibitor != nil {
			return nil
		}
		if im.compositor == nil || im.inhibitorMgr == nil {
			return fmt.Errorf("idle_inhibit not supported by compositor")
		}
		surface, _ := im.compositor.CreateSurface()
		inhibitor, err := im.inhibitorMgr.CreateInhibitor(surface)
		if err != nil {
			if surface != nil {
				surface.Destroy()
			}
			return err
		}
		inh.surface = surface
		inh.inhibitor = inhibitor
		return nil
	}

	if inh.inhibitor != nil {
		inh.inhibitor.Destroy()
		inh.inhibitor = nil
	}
	if inh.surface != nil {
		inh.surface.Destroy()
		inh.surface = nil
	}
	return nil
}

func (im *IdleManager) InhibitSystem(on bool) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	if im.systemInhibitor.enabled == on {
		return nil
	}

	if on {
		if im.systemInhibitor.cmd != nil {
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		cmd := exec.CommandContext(ctx, "systemd-inhibit",
			"--what=sleep:idle",
			"--who=axctl",
			"--why=IPC daemon running",
			"--mode=block",
			"sleep",
			"infinity",
		)
		cmd.Stdout = nil
		cmd.Stderr = nil

		if err := cmd.Start(); err != nil {
			cancel()
			return fmt.Errorf("failed to start systemd-inhibit: %w", err)
		}

		im.systemInhibitor.cmd = cmd
		im.systemInhibitor.ctx = ctx
		im.systemInhibitor.cancel = cancel
		im.systemInhibitor.enabled = true
		return nil
	}

	if im.systemInhibitor.cancel != nil {
		im.systemInhibitor.cancel()
		im.systemInhibitor.cancel = nil
	}
	if im.systemInhibitor.cmd != nil {
		im.systemInhibitor.cmd.Process.Kill()
		im.systemInhibitor.cmd.Wait()
		im.systemInhibitor.cmd = nil
	}
	im.systemInhibitor.ctx = nil
	im.systemInhibitor.enabled = false
	return nil
}

func (im *IdleManager) IsSystemInhibited() bool {
	im.mu.Lock()
	defer im.mu.Unlock()
	return im.systemInhibitor.enabled
}

// MediaInhibitorCheck checks for active audio/sink-inputs via PulseAudio/PipeWire.
// Returns a map with media info and count of active media streams.
func (im *IdleManager) MediaInhibitorCheck() (map[string]interface{}, error) {
	result := make(map[string]interface{})

	count, apps := checkMediaApps()
	result["count"] = count
	result["apps"] = apps

	return result, nil
}

func checkMediaApps() (int, []string) {
	cmd := exec.Command("pactl", "list", "sink-inputs")
	out, err := cmd.Output()
	if err != nil {
		return 0, nil
	}

	text := string(out)
	if strings.TrimSpace(text) == "" {
		return 0, nil
	}

	mediaBlacklist := []string{
		"speech-dispatcher",
		"speech-dispatcher-dummy",
		"sndio",
		"pipewire",
		"wireplumber",
		"galene",
	}

	var count int
	var block strings.Builder
	seen := make(map[string]bool)

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		isHeader := strings.HasPrefix(trimmed, "Sink Input #") || strings.HasPrefix(trimmed, "SinkInput #")

		if isHeader {
			if block.Len() > 0 && sinkInputCounts(block.String()) {
				if app := extractAppName(block.String()); app != "" && !seen[app] {
					if !isBlacklisted(app, mediaBlacklist) {
						seen[app] = true
						count++
					}
				}
			}
			block.Reset()
		}

		block.WriteString(line)
		block.WriteString("\n")
	}

	if block.Len() > 0 && sinkInputCounts(block.String()) {
		if app := extractAppName(block.String()); app != "" && !seen[app] {
			if !isBlacklisted(app, mediaBlacklist) {
				seen[app] = true
				count++
			}
		}
	}

	var apps []string
	for app := range seen {
		apps = append(apps, app)
	}

	return count, apps
}

func isBlacklisted(app string, blacklist []string) bool {
	lcApp := strings.ToLower(app)
	for _, b := range blacklist {
		if strings.Contains(lcApp, strings.ToLower(b)) {
			return true
		}
	}
	return false
}

func sinkInputCounts(block string) bool {
	lines := strings.Split(block, "\n")
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.EqualFold(t, "Corked: yes") {
			return false
		}
		if strings.EqualFold(t, "Mute: yes") {
			return false
		}
	}
	return true
}

func extractAppName(block string) string {
	lines := strings.Split(block, "\n")
	var inProps bool

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "Properties:" {
			inProps = true
			continue
		}

		if !inProps {
			continue
		}

		if trimmed == "" {
			continue
		}

		if !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, " ") {
			break
		}

		if strings.HasPrefix(trimmed, "application.name = ") {
			name := strings.Trim(strings.TrimPrefix(trimmed, "application.name = "), `"`)
			return name
		}
		if strings.HasPrefix(trimmed, "application.process.binary = ") {
			name := strings.Trim(strings.TrimPrefix(trimmed, "application.process.binary = "), `"`)
			return name
		}
		if strings.HasPrefix(trimmed, "media.name = ") {
			name := strings.Trim(strings.TrimPrefix(trimmed, "media.name = "), `"`)
			return name
		}
	}

	return ""
}

func (im *IdleManager) AppInhibitorCheck(patterns []string) (map[string]bool, error) {
	result := make(map[string]bool)

	if len(patterns) == 0 {
		patterns = []string{"vlc", "mpv", "firefox", "chromium", "chrome", "brave", "vivaldi", "steam"}
	}

	if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "" {
		count, apps := checkHyprlandApps(patterns)
		for _, app := range apps {
			result[app] = true
		}
		if count > 0 {
			return result, nil
		}
	}

	if os.Getenv("NIRI_SOCKET") != "" || os.Getenv("XDG_CURRENT_DESKTOP") == "niri" {
		count, apps := checkNiriApps(patterns)
		for _, app := range apps {
			result[app] = true
		}
		if count > 0 {
			return result, nil
		}
	}

	count, apps := checkProcApps(patterns)
	for _, app := range apps {
		result[app] = true
	}
	if count > 0 {
		return result, nil
	}

	return result, nil
}

func checkHyprlandApps(patterns []string) (int, []string) {
	cmd := exec.Command("hyprctl", "clients", "-j")
	out, err := cmd.Output()
	if err != nil {
		return 0, nil
	}

	var windows []struct {
		Class string `json:"class"`
	}
	if err := json.Unmarshal(out, &windows); err != nil {
		return 0, nil
	}

	var matches []string
	seen := make(map[string]bool)
	for _, w := range windows {
		if w.Class == "" {
			continue
		}
		lcClass := toLower(w.Class)
		for _, p := range patterns {
			if stringsContains(lcClass, toLower(p)) {
				if !seen[w.Class] {
					seen[w.Class] = true
					matches = append(matches, w.Class)
				}
				break
			}
		}
	}

	return len(matches), matches
}

// checkNiriApps checks app patterns against Niri windows using niri msg
func checkNiriApps(patterns []string) (int, []string) {
	cmd := exec.Command("niri", "msg", "windows")
	out, err := cmd.Output()
	if err != nil {
		return 0, nil
	}

	text := string(out)
	var matches []string
	seen := make(map[string]bool)

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "  App ID: ") {
			appID := strings.Trim(strings.TrimPrefix(line, "  App ID: "), `"`)
			if appID == "" {
				continue
			}
			lcAppID := toLower(appID)
			for _, p := range patterns {
				if stringsContains(lcAppID, toLower(p)) {
					if !seen[appID] {
						seen[appID] = true
						matches = append(matches, appID)
					}
					break
				}
			}
		}
	}

	return len(matches), matches
}

// checkProcApps checks app patterns against running processes via /proc
func checkProcApps(patterns []string) (int, []string) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, nil
	}

	var matches []string
	seen := make(map[string]bool)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		// Check if it's a PID (numeric directory)
		name := e.Name()
		if len(name) > 0 && name[0] >= '0' && name[0] <= '9' {
			// Try to read comm and exe
			commPath := "/proc/" + name + "/comm"
			if comm, err := os.ReadFile(commPath); err == nil {
				commStr := strings.TrimSpace(string(comm))
				lcComm := toLower(commStr)
				for _, p := range patterns {
					if stringsContains(lcComm, toLower(p)) {
						if !seen[commStr] {
							seen[commStr] = true
							matches = append(matches, commStr)
						}
						break
					}
				}
			}
		}
	}

	return len(matches), matches
}

// Helper functions
func toLower(s string) string {
	var result []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		result = append(result, c)
	}
	return string(result)
}

func stringsContains(haystack, needle string) bool {
	return len(haystack) >= len(needle) &&
		(len(needle) == 0 ||
			strings.Contains(haystack, needle) ||
			strings.HasPrefix(haystack, needle) ||
			strings.HasSuffix(haystack, needle))
}
