package notifications

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const (
	serviceName  = "org.freedesktop.Notifications"
	objectPath   = dbus.ObjectPath("/org/freedesktop/Notifications")
	interfaceID  = "org.freedesktop.Notifications"
	maxImageSize = 32 * 1024 * 1024
	maxHistory   = 1000
)

type Action struct {
	ID    string `json:"identifier"`
	Label string `json:"text"`
}

type Notification struct {
	ID              uint32   `json:"id"`
	AppName         string   `json:"appName"`
	AppIcon         string   `json:"appIcon,omitempty"`
	Summary         string   `json:"summary"`
	Body            string   `json:"body"`
	Image           string   `json:"image,omitempty"`
	Actions         []Action `json:"actions,omitempty"`
	Urgency         uint8    `json:"urgency"`
	Category        string   `json:"category,omitempty"`
	DesktopEntry    string   `json:"desktopEntry,omitempty"`
	ReplaceKey      string   `json:"replaceKey,omitempty"`
	OwnerToken      string   `json:"ownerToken,omitempty"`
	HistoryPriority int      `json:"historyPriority,omitempty"`
	Time            int64    `json:"time"`
	Timeout         int32    `json:"timeout"`
	Resident        bool     `json:"resident,omitempty"`
	Transient       bool     `json:"transient,omitempty"`
	Popup           bool     `json:"popup"`
}

type Event struct {
	Type         string       `json:"type"`
	Notification Notification `json:"notification"`
	ActionID     string       `json:"actionId,omitempty"`
	Reason       uint32       `json:"reason,omitempty"`
}

type Service struct {
	mu        sync.RWMutex
	conn      *dbus.Conn
	status    string
	statePath string
	imageDir  string
	active    map[uint32]Notification
	history   []Notification
	timers    map[uint32]*time.Timer
	deadlines map[uint32]time.Time
	remaining map[uint32]time.Duration
	listeners map[chan Event]struct{}
	nextID    uint32
	dnd       bool
	closed    bool
}

type imageData struct {
	Width         int32
	Height        int32
	RowStride     int32
	HasAlpha      bool
	BitsPerSample int32
	Channels      int32
	Data          []byte
}

func New() (*Service, error) {
	stateHome := os.Getenv("XDG_STATE_HOME")
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if stateHome == "" {
		stateHome = filepath.Join(home, ".local", "state")
	}
	if cacheHome == "" {
		cacheHome = filepath.Join(home, ".cache")
	}
	s := &Service{
		statePath: filepath.Join(stateHome, "axctl", "notifications.json"),
		imageDir:  filepath.Join(cacheHome, "axctl", "notifications", "images"),
		active:    make(map[uint32]Notification),
		timers:    make(map[uint32]*time.Timer),
		deadlines: make(map[uint32]time.Time),
		remaining: make(map[uint32]time.Duration),
		listeners: make(map[chan Event]struct{}),
		nextID:    1,
		status:    "stopped",
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) Start() error {
	if os.Getenv("AXCTL_NOTIFICATIONS") == "0" {
		s.mu.Lock()
		s.status = "disabled"
		s.mu.Unlock()
		return nil
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		s.setStatus("unavailable: " + err.Error())
		return err
	}
	reply, err := conn.RequestName(serviceName, dbus.NameFlagDoNotQueue)
	if err != nil {
		conn.Close()
		s.setStatus("unavailable: " + err.Error())
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		err = fmt.Errorf("D-Bus name %s is already owned", serviceName)
		s.setStatus("conflict: " + err.Error())
		return err
	}
	if err := conn.Export(s, objectPath, interfaceID); err != nil {
		_, _ = conn.ReleaseName(serviceName)
		conn.Close()
		s.setStatus("unavailable: " + err.Error())
		return err
	}
	introspection := introspect.NewIntrospectable(&introspect.Node{
		Name: string(objectPath),
		Interfaces: []introspect.Interface{{
			Name:    interfaceID,
			Methods: introspect.Methods(s),
			Signals: []introspect.Signal{
				{Name: "ActionInvoked", Args: []introspect.Arg{{Name: "id", Type: "u"}, {Name: "action_key", Type: "s"}}},
				{Name: "NotificationClosed", Args: []introspect.Arg{{Name: "id", Type: "u"}, {Name: "reason", Type: "u"}}},
			},
		}},
	})
	if err := conn.Export(introspection, objectPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		conn.Export(nil, objectPath, interfaceID)
		_, _ = conn.ReleaseName(serviceName)
		conn.Close()
		s.setStatus("unavailable: " + err.Error())
		return err
	}
	s.mu.Lock()
	s.conn = conn
	s.status = "available"
	s.mu.Unlock()
	return nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	for _, timer := range s.timers {
		timer.Stop()
	}
	s.timers = make(map[uint32]*time.Timer)
	conn := s.conn
	s.conn = nil
	for listener := range s.listeners {
		close(listener)
		delete(s.listeners, listener)
	}
	s.mu.Unlock()
	if conn == nil {
		return nil
	}
	_, releaseErr := conn.ReleaseName(serviceName)
	closeErr := conn.Close()
	if releaseErr != nil {
		return releaseErr
	}
	return closeErr
}

func (s *Service) Status() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *Service) Snapshot() []Notification {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Notification, len(s.history))
	copy(result, s.history)
	return result
}

func (s *Service) Active() []Notification {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Notification, 0, len(s.active))
	for _, notification := range s.active {
		result = append(result, notification)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Time < result[j].Time })
	return result
}

func (s *Service) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 128)
	s.mu.Lock()
	if s.closed {
		close(ch)
		s.mu.Unlock()
		return ch, func() {}
	}
	s.listeners[ch] = struct{}{}
	s.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			s.mu.Lock()
			if _, ok := s.listeners[ch]; ok {
				delete(s.listeners, ch)
				close(ch)
			}
			s.mu.Unlock()
		})
	}
}

func (s *Service) NotifyInternal(input Notification) (uint32, error) {
	if input.Time == 0 {
		input.Time = time.Now().UnixMilli()
	}
	if input.Timeout < 0 {
		input.Timeout = 5000
	}
	replacesID := uint32(0)
	if input.ReplaceKey != "" {
		s.mu.RLock()
		for id, current := range s.active {
			if current.ReplaceKey == input.ReplaceKey {
				replacesID = id
				break
			}
		}
		s.mu.RUnlock()
	}
	return s.insert(replacesID, input, input.Timeout)
}

func (s *Service) Dismiss(id uint32) {
	s.closeNotification(id, 2)
}

func (s *Service) CloseUser(id uint32) {
	s.closeNotification(id, 2)
}

func (s *Service) Pause(id uint32) {
	s.mu.Lock()
	if s.remaining == nil {
		s.remaining = make(map[uint32]time.Duration)
	}
	if s.deadlines == nil {
		s.deadlines = make(map[uint32]time.Time)
	}
	if timer := s.timers[id]; timer != nil {
		if timer.Stop() {
			remaining := time.Until(s.deadlines[id])
			if remaining < 0 {
				remaining = 0
			}
			s.remaining[id] = remaining
		}
		delete(s.timers, id)
		delete(s.deadlines, id)
	}
	s.mu.Unlock()
}

func (s *Service) Resume(id uint32) {
	s.mu.Lock()
	remaining, ok := s.remaining[id]
	if !ok || s.closed {
		s.mu.Unlock()
		return
	}
	delete(s.remaining, id)
	if remaining <= 0 {
		s.mu.Unlock()
		s.closeNotification(id, 1)
		return
	}
	s.scheduleTimerLocked(id, remaining)
	s.mu.Unlock()
}

func (s *Service) Delete(id uint32) {
	s.closeNotification(id, 2)
	s.mu.Lock()
	filtered := s.history[:0]
	for _, notification := range s.history {
		if notification.ID != id {
			filtered = append(filtered, notification)
		}
	}
	s.history = filtered
	err := s.saveLocked()
	s.mu.Unlock()
	s.publish(Event{Type: "deleted", Notification: Notification{ID: id}})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[notifications] persist delete: %v\n", err)
	}
}

func (s *Service) ClearAll() error {
	for _, notification := range s.Active() {
		s.closeNotification(notification.ID, 2)
	}
	return s.ClearHistory()
}

func (s *Service) ClearHistory() error {
	s.mu.Lock()
	s.history = nil
	err := s.saveLocked()
	s.mu.Unlock()
	s.publish(Event{Type: "history-cleared"})
	return err
}

func (s *Service) SetDoNotDisturb(enabled bool) {
	s.mu.Lock()
	s.dnd = enabled
	err := s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[notifications] persist do-not-disturb: %v\n", err)
	}
	s.publish(Event{Type: "dnd-changed"})
}

func (s *Service) DoNotDisturb() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dnd
}

func (s *Service) Notify(appName string, replacesID uint32, appIcon, summary, body string, actions []string, hints map[string]dbus.Variant, expireTimeout int32) (uint32, *dbus.Error) {
	parsedActions := make([]Action, 0, len(actions)/2)
	for i := 0; i+1 < len(actions); i += 2 {
		parsedActions = append(parsedActions, Action{ID: actions[i], Label: actions[i+1]})
	}
	input := Notification{
		AppName: appName,
		AppIcon: appIcon,
		Summary: summary,
		Body:    body,
		Actions: parsedActions,
		Urgency: 1,
		Popup:   true,
	}
	if urgency, ok := hintUint8(hints["urgency"]); ok {
		if urgency > 2 {
			urgency = 1
		}
		input.Urgency = urgency
	}
	input.Category, _ = hintString(hints["category"])
	input.DesktopEntry, _ = hintString(hints["desktop-entry"])
	input.Resident, _ = hintBool(hints["resident"])
	input.Transient, _ = hintBool(hints["transient"])
	input.Image, _ = s.imageFromHints(hints)
	if expireTimeout < -1 {
		expireTimeout = -1
	}
	if expireTimeout == -1 || expireTimeout == 0 {
		expireTimeout = 5000
		if input.Urgency == 2 {
			expireTimeout = 0
		}
	}
	input.Timeout = expireTimeout
	id, err := s.insert(replacesID, input, expireTimeout)
	if err != nil {
		return 0, dbus.MakeFailedError(err)
	}
	return id, nil
}

func (s *Service) CloseNotification(id uint32) *dbus.Error {
	s.closeNotification(id, 3)
	return nil
}

func (s *Service) GetCapabilities() ([]string, *dbus.Error) {
	return []string{"actions", "body", "body-markup", "icon-static", "persistence"}, nil
}

func (s *Service) GetServerInformation() (string, string, string, string, *dbus.Error) {
	return "axctl", "Axenide", "1", "1.3", nil
}

func (s *Service) InvokeAction(id uint32, actionID string) error {
	s.mu.RLock()
	notification, ok := s.active[id]
	conn := s.conn
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("notification %d is not active", id)
	}
	valid := false
	for _, action := range notification.Actions {
		if action.ID == actionID {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("unknown action %q", actionID)
	}
	if conn != nil {
		if err := conn.Emit(objectPath, interfaceID+".ActionInvoked", id, actionID); err != nil {
			return err
		}
	}
	s.publish(Event{Type: "action-invoked", Notification: notification, ActionID: actionID})
	if !notification.Resident {
		s.closeNotification(id, 2)
	}
	return nil
}

func (s *Service) insert(replacesID uint32, input Notification, timeout int32) (uint32, error) {
	if input.AppName == "" {
		input.AppName = "Unknown"
	}
	if input.Time == 0 {
		input.Time = time.Now().UnixMilli()
	}
	if input.Actions == nil {
		input.Actions = []Action{}
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, errors.New("notification service is closed")
	}
	id := replacesID
	old, replacing := s.active[id]
	if id == 0 || !replacing {
		id = s.nextID
		if id == 0 {
			id = 1
		}
		if _, exists := s.active[id]; exists {
			for {
				id++
				if id == 0 {
					id = 1
				}
				if _, exists := s.active[id]; !exists {
					break
				}
			}
		}
		s.nextID = id + 1
		if s.nextID == 0 {
			s.nextID = 1
		}
	}
	input.ID = id
	input.Popup = !s.dnd && input.Popup
	input.Timeout = timeout
	s.active[id] = input
	if replacing {
		if timer := s.timers[id]; timer != nil {
			timer.Stop()
			delete(s.timers, id)
		}
		delete(s.deadlines, id)
		delete(s.remaining, id)
		for index := range s.history {
			if s.history[index].ID == old.ID {
				s.history[index] = input
			}
		}
	} else {
		s.history = append(s.history, input)
	}
	if len(s.history) > maxHistory {
		s.history = append([]Notification(nil), s.history[len(s.history)-maxHistory:]...)
	}
	if timeout > 0 {
		s.scheduleTimerLocked(id, time.Duration(timeout)*time.Millisecond)
	}
	err := s.saveLocked()
	s.mu.Unlock()
	s.publish(Event{Type: "received", Notification: input})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[notifications] persist history: %v\n", err)
	}
	return id, nil
}

func (s *Service) closeNotification(id, reason uint32) {
	s.mu.Lock()
	notification, ok := s.active[id]
	if !ok {
		s.mu.Unlock()
		return
	}
	delete(s.active, id)
	if timer := s.timers[id]; timer != nil {
		timer.Stop()
		delete(s.timers, id)
	}
	delete(s.deadlines, id)
	delete(s.remaining, id)
	notification.Popup = false
	for index := range s.history {
		if s.history[index].ID == id {
			s.history[index].Popup = false
			break
		}
	}
	err := s.saveLocked()
	conn := s.conn
	s.mu.Unlock()
	if conn != nil {
		if emitErr := conn.Emit(objectPath, interfaceID+".NotificationClosed", id, reason); emitErr != nil {
			fmt.Fprintf(os.Stderr, "[notifications] emit close signal: %v\n", emitErr)
		}
	}
	s.publish(Event{Type: "closed", Notification: notification, Reason: reason})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[notifications] persist close: %v\n", err)
	}
}

func (s *Service) scheduleTimerLocked(id uint32, duration time.Duration) {
	if s.timers == nil {
		s.timers = make(map[uint32]*time.Timer)
	}
	if s.deadlines == nil {
		s.deadlines = make(map[uint32]time.Time)
	}
	s.deadlines[id] = time.Now().Add(duration)
	s.timers[id] = time.AfterFunc(duration, func() { s.closeNotification(id, 1) })
}

func (s *Service) imageFromHints(hints map[string]dbus.Variant) (string, error) {
	for _, key := range []string{"image-path", "image_path", "imagefile"} {
		if value, ok := hintString(hints[key]); ok && value != "" {
			if strings.HasPrefix(value, "file://") {
				value = strings.TrimPrefix(value, "file://")
			}
			if filepath.IsAbs(value) {
				if info, err := os.Stat(value); err == nil && !info.IsDir() {
					return value, nil
				}
			}
		}
	}
	for _, key := range []string{"image-data", "image_data", "icon-data", "icon_data"} {
		variant, ok := hints[key]
		if !ok {
			continue
		}
		var data imageData
		if err := variant.Store(&data); err != nil {
			return "", err
		}
		return s.saveImageData(data)
	}
	return "", nil
}

func (s *Service) saveImageData(data imageData) (string, error) {
	if data.Width < 1 || data.Height < 1 || data.Width > 4096 || data.Height > 4096 || data.BitsPerSample != 8 || (data.Channels != 3 && data.Channels != 4) {
		return "", fmt.Errorf("invalid notification image dimensions or format")
	}
	rowBytes := int64(data.Width) * int64(data.Channels)
	if int64(data.RowStride) < rowBytes || int64(data.RowStride)*int64(data.Height) > maxImageSize || int64(len(data.Data)) < int64(data.RowStride)*int64(data.Height) {
		return "", fmt.Errorf("notification image data exceeds valid bounds")
	}
	img := image.NewRGBA(image.Rect(0, 0, int(data.Width), int(data.Height)))
	for y := 0; y < int(data.Height); y++ {
		row := data.Data[y*int(data.RowStride) : y*int(data.RowStride)+int(rowBytes)]
		for x := 0; x < int(data.Width); x++ {
			offset := x * int(data.Channels)
			alpha := uint8(255)
			if data.Channels == 4 && data.HasAlpha {
				alpha = row[offset+3]
			}
			img.SetRGBA(x, y, color.RGBA{R: row[offset], G: row[offset+1], B: row[offset+2], A: alpha})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		return "", err
	}
	content := encoded.Bytes()
	if len(content) > maxImageSize {
		return "", fmt.Errorf("encoded notification image is too large")
	}
	hash := sha256.Sum256(content)
	name := hex.EncodeToString(hash[:]) + ".png"
	if err := os.MkdirAll(s.imageDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(s.imageDir, name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Service) publish(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for listener := range s.listeners {
		select {
		case listener <- event:
		default:
			close(listener)
			delete(s.listeners, listener)
		}
	}
}

func (s *Service) setStatus(status string) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

func (s *Service) load() error {
	data, err := os.ReadFile(s.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var persisted struct {
		History []Notification `json:"history"`
		DND     bool           `json:"dnd"`
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		return fmt.Errorf("parse notification history: %w", err)
	}
	s.history = persisted.History
	s.dnd = persisted.DND
	for index := range s.history {
		s.history[index].Popup = false
	}
	if len(s.history) > maxHistory {
		s.history = s.history[len(s.history)-maxHistory:]
	}
	for _, notification := range s.history {
		if notification.ID >= s.nextID {
			s.nextID = notification.ID + 1
			if s.nextID == 0 {
				s.nextID = 1
			}
		}
	}
	return nil
}

func (s *Service) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.statePath), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		History []Notification `json:"history"`
		DND     bool           `json:"dnd"`
	}{History: s.history, DND: s.dnd}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.statePath), ".notifications-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.statePath)
}

func hintString(value dbus.Variant) (string, bool) {
	result, ok := value.Value().(string)
	return result, ok
}

func hintBool(value dbus.Variant) (bool, bool) {
	result, ok := value.Value().(bool)
	return result, ok
}

func hintUint8(value dbus.Variant) (uint8, bool) {
	switch result := value.Value().(type) {
	case uint8:
		return result, true
	case int32:
		if result >= 0 && result <= 255 {
			return uint8(result), true
		}
	}
	return 0, false
}
