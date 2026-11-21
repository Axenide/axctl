package tray

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
	"github.com/godbus/dbus/v5/prop"
)

const (
	watcherName      = "org.kde.StatusNotifierWatcher"
	watcherPath      = dbus.ObjectPath("/StatusNotifierWatcher")
	watcherInterface = "org.kde.StatusNotifierWatcher"
	itemInterface    = "org.kde.StatusNotifierItem"
	menuInterface    = "com.canonical.dbusmenu"
)

type Item struct {
	ID           string `json:"id"`
	Service      string `json:"service"`
	ObjectPath   string `json:"objectPath"`
	Category     string `json:"category"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	IconName     string `json:"iconName,omitempty"`
	IconPath     string `json:"iconPath,omitempty"`
	OverlayIcon  string `json:"overlayIcon,omitempty"`
	ToolTipTitle string `json:"toolTipTitle,omitempty"`
	ToolTip      string `json:"toolTip,omitempty"`
	Menu         string `json:"menu,omitempty"`
	ItemIsMenu   bool   `json:"itemIsMenu"`
}

type Event struct {
	Type  string `json:"type"`
	ID    string `json:"id,omitempty"`
	Items []Item `json:"items,omitempty"`
}

type ItemRef struct {
	Service string          `json:"service"`
	Owner   string          `json:"owner"`
	Path    dbus.ObjectPath `json:"path"`
}

type IconPixmap struct {
	Width  int32
	Height int32
	Data   []byte
}

type tooltip struct {
	IconName string
	IconData []IconPixmap
	Title    string
	Text     string
}

type MenuItem struct {
	ID          int32      `json:"id"`
	Label       string     `json:"label,omitempty"`
	Enabled     bool       `json:"enabled"`
	Visible     bool       `json:"visible"`
	IconName    string     `json:"iconName,omitempty"`
	IconData    string     `json:"iconData,omitempty"`
	Type        string     `json:"type,omitempty"`
	ToggleType  string     `json:"toggleType,omitempty"`
	ToggleState int32      `json:"toggleState,omitempty"`
	Children    []MenuItem `json:"children,omitempty"`
}

type menuNode struct {
	ID         int32
	Properties map[string]dbus.Variant
	Children   []dbus.Variant
}

type Service struct {
	mu           sync.RWMutex
	persistMu    sync.Mutex
	conn         *dbus.Conn
	properties   *prop.Properties
	ownsWatcher  bool
	watcherOwner string
	status       string
	items        map[string]Item
	refs         map[string]ItemRef
	hosts        map[string]struct{}
	listeners    map[chan Event]struct{}
	stop         chan struct{}
	closed       bool
	cacheDir     string
	statePath    string
}

func New() (*Service, error) {
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		cacheHome = filepath.Join(home, ".cache")
	}
	cacheDir := filepath.Join(cacheHome, "axctl", "tray")
	statePath := filepath.Join(cacheDir, "registrations.json")
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		statePath = filepath.Join(runtimeDir, "axctl", "tray-items.json")
	}
	return &Service{
		status:    "stopped",
		items:     make(map[string]Item),
		refs:      make(map[string]ItemRef),
		hosts:     make(map[string]struct{}),
		listeners: make(map[chan Event]struct{}),
		stop:      make(chan struct{}),
		cacheDir:  cacheDir,
		statePath: statePath,
	}, nil
}

func (s *Service) Start() error {
	if os.Getenv("AXCTL_TRAY") == "0" {
		s.setStatus("disabled")
		return nil
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		s.setStatus("unavailable: " + err.Error())
		return err
	}
	props, err := s.exportWatcher(conn)
	if err != nil {
		conn.Close()
		s.setStatus("unavailable: " + err.Error())
		return err
	}
	ch := make(chan *dbus.Signal, 128)
	conn.Signal(ch)
	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
	); err != nil {
		conn.RemoveSignal(ch)
		s.unexportWatcher(conn)
		conn.Close()
		s.setStatus("unavailable: " + err.Error())
		return err
	}
	s.mu.Lock()
	s.conn = conn
	s.properties = props
	s.mu.Unlock()
	reply, err := conn.RequestName(watcherName, dbus.NameFlagDoNotQueue)
	if err != nil {
		conn.RemoveSignal(ch)
		s.unexportWatcher(conn)
		conn.Close()
		s.mu.Lock()
		s.conn = nil
		s.properties = nil
		s.mu.Unlock()
		s.setStatus("unavailable: " + err.Error())
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		owner, ownerErr := s.nameOwner(watcherName)
		if ownerErr != nil || owner == "" {
			conn.RemoveSignal(ch)
			s.unexportWatcher(conn)
			conn.Close()
			s.mu.Lock()
			s.conn = nil
			s.properties = nil
			s.mu.Unlock()
			if ownerErr != nil {
				err = ownerErr
			} else {
				err = fmt.Errorf("D-Bus name %s has no owner", watcherName)
			}
			s.setStatus("conflict: " + err.Error())
			return err
		}
		if matchErr := s.watchWatcher(owner); matchErr != nil {
			fmt.Fprintf(os.Stderr, "[tray] watch existing watcher: %v\n", matchErr)
		}
		s.mu.Lock()
		s.watcherOwner = owner
		s.status = "available"
		s.mu.Unlock()
		go s.signalLoop(ch)
		s.registerAsHost(owner)
		s.syncWatcherItems(owner)
		return nil
	}
	s.mu.Lock()
	s.ownsWatcher = true
	s.watcherOwner = string(conn.Names()[0])
	s.status = "available"
	s.mu.Unlock()
	go s.signalLoop(ch)
	s.registerAsHost(watcherName)
	s.restoreItems()
	return nil
}

func (s *Service) exportWatcher(conn *dbus.Conn) (*prop.Properties, error) {
	if err := conn.Export(s, watcherPath, watcherInterface); err != nil {
		return nil, err
	}
	props, err := prop.Export(conn, watcherPath, prop.Map{
		watcherInterface: {
			"RegisteredStatusNotifierItems":  &prop.Prop{Value: []string{}, Emit: prop.EmitTrue},
			"IsStatusNotifierHostRegistered": &prop.Prop{Value: true, Emit: prop.EmitTrue},
			"ProtocolVersion":                &prop.Prop{Value: int32(0), Emit: prop.EmitConst},
		},
	})
	if err != nil {
		s.unexportWatcher(conn)
		return nil, err
	}
	introspection := introspect.NewIntrospectable(&introspect.Node{
		Name: string(watcherPath),
		Interfaces: []introspect.Interface{{
			Name:    watcherInterface,
			Methods: introspect.Methods(s),
			Properties: []introspect.Property{
				{Name: "RegisteredStatusNotifierItems", Type: "as", Access: "read"},
				{Name: "IsStatusNotifierHostRegistered", Type: "b", Access: "read"},
				{Name: "ProtocolVersion", Type: "i", Access: "read"},
			},
			Signals: []introspect.Signal{
				{Name: "StatusNotifierItemRegistered", Args: []introspect.Arg{{Name: "service", Type: "s"}}},
				{Name: "StatusNotifierItemUnregistered", Args: []introspect.Arg{{Name: "service", Type: "s"}}},
				{Name: "StatusNotifierHostRegistered"},
				{Name: "StatusNotifierHostUnregistered"},
			},
		}, prop.IntrospectData},
	})
	if err := conn.Export(introspection, watcherPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		s.unexportWatcher(conn)
		return nil, err
	}
	return props, nil
}

func (s *Service) unexportWatcher(conn *dbus.Conn) {
	conn.Export(nil, watcherPath, watcherInterface)
	conn.Export(nil, watcherPath, "org.freedesktop.DBus.Properties")
	conn.Export(nil, watcherPath, "org.freedesktop.DBus.Introspectable")
}

func (s *Service) nameOwner(name string) (string, error) {
	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn == nil {
		return "", fmt.Errorf("tray watcher is not connected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	call := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, name)
	if call.Err != nil {
		return "", call.Err
	}
	var owner string
	if err := call.Store(&owner); err != nil {
		return "", err
	}
	return owner, nil
}

func (s *Service) watchWatcher(owner string) error {
	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn == nil {
		return fmt.Errorf("tray watcher is not connected")
	}
	return conn.AddMatchSignal(
		dbus.WithMatchSender(owner),
		dbus.WithMatchObjectPath(watcherPath),
		dbus.WithMatchInterface(watcherInterface),
	)
}

func (s *Service) registerAsHost(destination string) {
	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	call := conn.Object(destination, watcherPath).CallWithContext(ctx, watcherInterface+".RegisterStatusNotifierHost", 0, string(conn.Names()[0]))
	if call.Err != nil {
		fmt.Fprintf(os.Stderr, "[tray] register host with %s: %v\n", destination, call.Err)
	}
}

func (s *Service) syncWatcherItems(owner string) int {
	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn == nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var value dbus.Variant
	call := conn.Object(owner, watcherPath).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, watcherInterface, "RegisteredStatusNotifierItems")
	if call.Err != nil {
		fmt.Fprintf(os.Stderr, "[tray] read items from watcher %s: %v\n", owner, call.Err)
		return 0
	}
	if err := call.Store(&value); err != nil {
		fmt.Fprintf(os.Stderr, "[tray] decode watcher items from %s: %v\n", owner, err)
		return 0
	}
	var addresses []string
	if err := value.Store(&addresses); err != nil {
		fmt.Fprintf(os.Stderr, "[tray] decode watcher item list from %s: %v\n", owner, err)
		return 0
	}
	registered := 0
	for _, address := range addresses {
		if s.trackExistingItem(address) {
			registered++
		}
	}
	return registered
}

func (s *Service) trackExistingItem(address string) bool {
	ref, err := s.itemRefFromAddress(address)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[tray] invalid item address %q: %v\n", address, err)
		return false
	}
	return s.trackResolvedItem(ref)
}

func (s *Service) trackResolvedItem(ref ItemRef) bool {
	id := itemID(ref)
	s.mu.RLock()
	_, exists := s.refs[id]
	_, loaded := s.items[id]
	s.mu.RUnlock()
	if exists {
		if !loaded {
			s.refreshItemAsync(id)
		}
		return false
	}
	if err := s.watchItem(ref); err != nil {
		fmt.Fprintf(os.Stderr, "[tray] watch existing item %s: %v\n", id, err)
	}
	s.mu.Lock()
	if _, exists := s.refs[id]; exists {
		s.mu.Unlock()
		return false
	}
	s.refs[id] = ref
	s.mu.Unlock()
	s.updateWatcherProperties(s.registeredItems())
	s.mu.RLock()
	conn := s.conn
	ownsWatcher := s.ownsWatcher
	s.mu.RUnlock()
	if ownsWatcher && conn != nil {
		_ = conn.Emit(watcherPath, watcherInterface+".StatusNotifierItemRegistered", itemAddress(ref))
	}
	if err := s.persistRefs(); err != nil {
		fmt.Fprintf(os.Stderr, "[tray] persist registrations: %v\n", err)
	}
	s.refreshItemAsync(id)
	return true
}

func (s *Service) itemRefFromAddress(address string) (ItemRef, error) {
	service, path, err := itemAddressParts(address)
	if err != nil {
		return ItemRef{}, err
	}
	owner, err := s.nameOwner(service)
	if err != nil {
		return ItemRef{}, err
	}
	return ItemRef{Service: service, Owner: owner, Path: path}, nil
}

func itemAddressParts(address string) (string, dbus.ObjectPath, error) {
	if strings.HasPrefix(address, "/") {
		return "", "", fmt.Errorf("item address does not include a service")
	}
	service := address
	path := dbus.ObjectPath("/StatusNotifierItem")
	if separator := strings.IndexByte(address, '/'); separator >= 0 {
		service = address[:separator]
		path = dbus.ObjectPath(address[separator:])
	}
	if !dbusNameValid(service) || !path.IsValid() {
		return "", "", fmt.Errorf("invalid service or object path")
	}
	return service, path, nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.stop)
	conn := s.conn
	ownsWatcher := s.ownsWatcher
	listeners := make([]chan Event, 0, len(s.listeners))
	for listener := range s.listeners {
		listeners = append(listeners, listener)
		delete(s.listeners, listener)
	}
	s.mu.Unlock()
	for _, listener := range listeners {
		close(listener)
	}
	if conn == nil {
		return nil
	}
	if err := s.persistRefs(); err != nil {
		fmt.Fprintf(os.Stderr, "[tray] persist registrations: %v\n", err)
	}
	if ownsWatcher {
		_, _ = conn.ReleaseName(watcherName)
	}
	return conn.Close()
}

func (s *Service) restoreItems() {
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "[tray] read registrations: %v\n", err)
		}
		return
	}
	var refs []ItemRef
	if err := json.Unmarshal(data, &refs); err != nil {
		fmt.Fprintf(os.Stderr, "[tray] decode registrations: %v\n", err)
		return
	}
	for _, ref := range refs {
		s.restoreItem(ref)
	}
	s.updateWatcherProperties(s.registeredItems())
	if err := s.persistRefs(); err != nil {
		fmt.Fprintf(os.Stderr, "[tray] persist registrations: %v\n", err)
	}
}

func (s *Service) restoreItem(ref ItemRef) bool {
	if ref.Service == "" || ref.Owner == "" || !ref.Path.IsValid() || !s.ownerMatches(ref) {
		return false
	}
	id := itemID(ref)
	s.mu.RLock()
	_, exists := s.refs[id]
	s.mu.RUnlock()
	if exists {
		return false
	}
	if err := s.watchItem(ref); err != nil {
		fmt.Fprintf(os.Stderr, "[tray] watch restored item %s: %v\n", id, err)
	}
	s.mu.Lock()
	if _, exists := s.refs[id]; exists {
		s.mu.Unlock()
		return false
	}
	s.refs[id] = ref
	s.mu.Unlock()
	s.updateWatcherProperties(s.registeredItems())
	if conn := s.conn; conn != nil {
		_ = conn.Emit(watcherPath, watcherInterface+".StatusNotifierItemRegistered", itemAddress(ref))
	}
	s.refreshItemAsync(id)
	return true
}

func (s *Service) ownerMatches(ref ItemRef) bool {
	conn := s.conn
	if conn == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	call := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, ref.Service)
	if call.Err != nil {
		return false
	}
	var owner string
	return call.Store(&owner) == nil && owner == ref.Owner
}

func (s *Service) persistRefs() error {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	s.mu.RLock()
	refs := make([]ItemRef, 0, len(s.refs))
	for _, ref := range s.refs {
		refs = append(refs, ref)
	}
	s.mu.RUnlock()
	sort.Slice(refs, func(i, j int) bool {
		return itemID(refs[i]) < itemID(refs[j])
	})
	data, err := json.Marshal(refs)
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.statePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".tray-items-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, s.statePath)
}

func (s *Service) Status() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *Service) List() []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Item, 0, len(s.items))
	for _, item := range s.items {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Category != result[j].Category {
			return result[i].Category < result[j].Category
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func (s *Service) registeredItems() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.registeredItemsLocked()
}

func (s *Service) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
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
			if _, exists := s.listeners[ch]; exists {
				delete(s.listeners, ch)
				close(ch)
			}
			s.mu.Unlock()
		})
	}
}

func (s *Service) RegisterStatusNotifierItem(sender dbus.Sender, service string) *dbus.Error {
	address := service
	path := dbus.ObjectPath("/StatusNotifierItem")
	if address == "" {
		service = string(sender)
	} else if strings.HasPrefix(address, "/") {
		service = string(sender)
		path = dbus.ObjectPath(address)
	} else if strings.Contains(address, "/") {
		parts := strings.SplitN(address, "/", 2)
		service = parts[0]
		path = dbus.ObjectPath("/" + parts[1])
	}
	if !path.IsValid() || !strings.HasPrefix(service, ":") && !dbusNameValid(service) {
		return dbus.MakeFailedError(fmt.Errorf("invalid status notifier item address"))
	}
	ref := ItemRef{Service: service, Owner: string(sender), Path: path}
	id := itemID(ref)
	s.mu.Lock()
	if _, exists := s.refs[id]; exists {
		s.mu.Unlock()
		return nil
	}
	s.refs[id] = ref
	s.mu.Unlock()
	if err := s.watchItem(ref); err != nil {
		fmt.Fprintf(os.Stderr, "[tray] watch item %s: %v\n", id, err)
	}
	s.updateWatcherProperties(s.registeredItems())
	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn != nil {
		_ = conn.Emit(watcherPath, watcherInterface+".StatusNotifierItemRegistered", itemAddress(ref))
	}
	if err := s.persistRefs(); err != nil {
		fmt.Fprintf(os.Stderr, "[tray] persist registrations: %v\n", err)
	}
	s.refreshItemAsync(id)
	return nil
}

func (s *Service) refreshItemAsync(id string) {
	go func() {
		for attempt := 0; attempt < 3; attempt++ {
			if err := s.refreshItem(id); err == nil {
				s.publish(Event{Type: "changed", Items: s.List()})
				return
			} else if attempt == 2 {
				fmt.Fprintf(os.Stderr, "[tray] read item %s: %v\n", id, err)
				return
			} else {
				time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
			}
		}
	}()
}

func (s *Service) RegisterStatusNotifierHost(sender dbus.Sender, service string) *dbus.Error {
	name := service
	if name == "" {
		name = string(sender)
	}
	s.mu.Lock()
	s.hosts[name] = struct{}{}
	first := len(s.hosts) == 1
	conn := s.conn
	s.mu.Unlock()
	s.updateHostRegisteredProperty()
	if first && conn != nil {
		_ = conn.Emit(watcherPath, watcherInterface+".StatusNotifierHostRegistered")
	}
	return nil
}

func (s *Service) IsStatusNotifierHostRegistered() (bool, *dbus.Error) {
	return true, nil
}

func (s *Service) RegisteredStatusNotifierItems() ([]string, *dbus.Error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.registeredItemsLocked(), nil
}

func (s *Service) ProtocolVersion() (int32, *dbus.Error) {
	return 0, nil
}

func (s *Service) Activate(id string, x, y int32) error {
	return s.callItem(id, "Activate", x, y)
}

func (s *Service) SecondaryActivate(id string, x, y int32) error {
	return s.callItem(id, "SecondaryActivate", x, y)
}

func (s *Service) ContextMenu(id string, x, y int32) error {
	return s.callItem(id, "ContextMenu", x, y)
}

func (s *Service) Scroll(id string, delta int32, orientation string) error {
	if orientation != "horizontal" {
		orientation = "vertical"
	}
	return s.callItem(id, "Scroll", delta, orientation)
}

func (s *Service) Menu(id string) (uint32, []MenuItem, error) {
	s.mu.RLock()
	item, exists := s.items[id]
	ref := s.refs[id]
	s.mu.RUnlock()
	if !exists || item.Menu == "" {
		return 0, nil, fmt.Errorf("tray item %q has no menu", id)
	}
	path := dbus.ObjectPath(item.Menu)
	if !path.IsValid() {
		return 0, nil, fmt.Errorf("tray item %q has invalid menu path", id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	obj := s.conn.Object(ref.Service, path)
	_ = obj.CallWithContext(ctx, menuInterface+".AboutToShow", 0, int32(0))
	var revision uint32
	var layout menuNode
	call := obj.CallWithContext(ctx, menuInterface+".GetLayout", 0, int32(0), int32(-1), []string{})
	if call.Err != nil {
		return 0, nil, call.Err
	}
	if err := call.Store(&revision, &layout); err != nil {
		return 0, nil, err
	}
	return revision, s.menuChildren(layout.Children), nil
}

func (s *Service) MenuEvent(id string, menuID int32, event string) error {
	s.mu.RLock()
	item, exists := s.items[id]
	ref := s.refs[id]
	s.mu.RUnlock()
	if !exists || item.Menu == "" {
		return fmt.Errorf("tray item %q has no menu", id)
	}
	path := dbus.ObjectPath(item.Menu)
	if !path.IsValid() {
		return fmt.Errorf("tray item %q has invalid menu path", id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	obj := s.conn.Object(ref.Service, path)
	return obj.CallWithContext(ctx, menuInterface+".Event", 0, menuID, event, dbus.MakeVariant(int32(0)), uint32(time.Now().UnixMilli())).Err
}

func (s *Service) callItem(id, method string, args ...any) error {
	s.mu.RLock()
	ref, exists := s.refs[id]
	s.mu.RUnlock()
	if !exists {
		return fmt.Errorf("tray item %q not found", id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.conn.Object(ref.Service, ref.Path).CallWithContext(ctx, itemInterface+"."+method, 0, args...).Err
}

func (s *Service) watchItem(ref ItemRef) error {
	conn := s.conn
	if conn == nil {
		return fmt.Errorf("tray watcher is not connected")
	}
	return conn.AddMatchSignal(
		dbus.WithMatchSender(ref.Owner),
		dbus.WithMatchObjectPath(ref.Path),
		dbus.WithMatchInterface(itemInterface),
	)
}

func (s *Service) signalLoop(ch chan *dbus.Signal) {
	for {
		select {
		case <-s.stop:
			return
		case signal, ok := <-ch:
			if !ok {
				return
			}
			if signal == nil {
				continue
			}
			if signal.Name == "org.freedesktop.DBus.NameOwnerChanged" && len(signal.Body) >= 3 {
				name, _ := signal.Body[0].(string)
				owner, _ := signal.Body[1].(string)
				newOwner, _ := signal.Body[2].(string)
				if name == watcherName {
					s.handleWatcherOwnerChanged(newOwner)
					continue
				}
				if strings.HasPrefix(name, ":") && owner != "" && newOwner == "" {
					s.removeOwner(name)
				}
				continue
			}
			if signal.Name == watcherInterface+".StatusNotifierItemRegistered" && len(signal.Body) > 0 {
				address, _ := signal.Body[0].(string)
				if s.isExistingWatcherSignal(signal.Sender) {
					s.trackExistingItem(address)
				}
				continue
			}
			if signal.Name == watcherInterface+".StatusNotifierItemUnregistered" && len(signal.Body) > 0 {
				address, _ := signal.Body[0].(string)
				if s.isExistingWatcherSignal(signal.Sender) {
					s.removeExistingItem(address)
				}
				continue
			}
			if signal.Name == itemInterface+".NewTitle" || signal.Name == itemInterface+".NewIcon" || signal.Name == itemInterface+".NewAttentionIcon" || signal.Name == itemInterface+".NewOverlayIcon" || signal.Name == itemInterface+".NewToolTip" || signal.Name == itemInterface+".NewStatus" || signal.Name == itemInterface+".NewMenu" || signal.Name == itemInterface+".NewItemIsMenu" {
				id := itemID(ItemRef{Service: signal.Sender, Path: signal.Path})
				if id == "" {
					s.mu.RLock()
					for candidate, ref := range s.refs {
						if ref.Path == signal.Path && ref.Owner == signal.Sender {
							id = candidate
							break
						}
					}
					s.mu.RUnlock()
				}
				if id != "" {
					_ = s.refreshItem(id)
					s.publish(Event{Type: "changed", Items: s.List()})
				}
			}
		}
	}
}

func (s *Service) isExistingWatcherSignal(sender string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.ownsWatcher && sender != "" && sender == s.watcherOwner
}

func (s *Service) handleWatcherOwnerChanged(newOwner string) {
	s.mu.RLock()
	conn := s.conn
	currentOwner := s.watcherOwner
	ownsWatcher := s.ownsWatcher
	closed := s.closed
	s.mu.RUnlock()
	if conn == nil || closed {
		return
	}
	self := string(conn.Names()[0])
	if newOwner == self {
		if ownsWatcher && currentOwner == self {
			return
		}
		s.mu.Lock()
		s.ownsWatcher = true
		s.watcherOwner = self
		s.status = "available"
		s.mu.Unlock()
		s.registerAsHost(watcherName)
		s.updateWatcherProperties(s.registeredItems())
		for _, address := range s.registeredItems() {
			_ = conn.Emit(watcherPath, watcherInterface+".StatusNotifierItemRegistered", address)
		}
		s.restoreItems()
		return
	}
	if newOwner == "" {
		reply, err := conn.RequestName(watcherName, dbus.NameFlagDoNotQueue)
		if err == nil && reply == dbus.RequestNameReplyPrimaryOwner {
			s.handleWatcherOwnerChanged(self)
			return
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "[tray] acquire watcher name: %v\n", err)
		}
		newOwner, err = s.nameOwner(watcherName)
		if err != nil {
			return
		}
	}
	if newOwner == "" {
		return
	}
	if currentOwner != "" && currentOwner != self && currentOwner != newOwner {
		_ = conn.RemoveMatchSignal(
			dbus.WithMatchSender(currentOwner),
			dbus.WithMatchObjectPath(watcherPath),
			dbus.WithMatchInterface(watcherInterface),
		)
	}
	if err := s.watchWatcher(newOwner); err != nil {
		fmt.Fprintf(os.Stderr, "[tray] watch existing watcher: %v\n", err)
	}
	s.mu.Lock()
	s.ownsWatcher = false
	s.watcherOwner = newOwner
	s.status = "available"
	s.mu.Unlock()
	s.registerAsHost(newOwner)
	s.syncWatcherItems(newOwner)
}

func (s *Service) removeExistingItem(address string) {
	service, path, err := itemAddressParts(address)
	if err != nil {
		return
	}
	id := service + string(path)
	s.mu.RLock()
	if _, exists := s.refs[id]; !exists {
		for candidate, ref := range s.refs {
			if ref.Path == path && (ref.Service == service || ref.Owner == service) {
				id = candidate
				break
			}
		}
	}
	_, exists := s.refs[id]
	s.mu.RUnlock()
	if exists {
		s.removeItem(id, true)
	}
}

func (s *Service) refreshItem(id string) error {
	s.mu.RLock()
	ref, exists := s.refs[id]
	conn := s.conn
	s.mu.RUnlock()
	if !exists || conn == nil {
		return fmt.Errorf("tray item %q not found", id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var values map[string]dbus.Variant
	if err := conn.Object(ref.Service, ref.Path).CallWithContext(ctx, "org.freedesktop.DBus.Properties.GetAll", 0, itemInterface).Store(&values); err != nil {
		return err
	}
	item := Item{
		ID:         id,
		Service:    ref.Service,
		ObjectPath: string(ref.Path),
		Category:   variantString(values["Category"], "ApplicationStatus"),
		Title:      variantString(values["Title"], ""),
		Status:     variantString(values["Status"], "Active"),
		IconName:   variantString(values["IconName"], ""),
		Menu:       variantObjectPath(values["Menu"]),
		ItemIsMenu: variantBool(values["ItemIsMenu"]),
	}
	if iconPath := variantString(values["IconName"], ""); filepath.IsAbs(iconPath) {
		item.IconPath = iconPath
	}
	if tooltipValue, ok := values["ToolTip"]; ok {
		var tip tooltip
		if tooltipValue.Store(&tip) == nil {
			item.ToolTipTitle = tip.Title
			item.ToolTip = tip.Text
			if item.IconName == "" {
				item.IconName = tip.IconName
			}
			if item.IconName == "" && len(tip.IconData) > 0 {
				item.IconPath, _ = s.savePixmap(tip.IconData)
			}
		}
	}
	if item.IconName == "" {
		if path, err := s.savePixmapVariant(values["IconPixmap"]); err == nil {
			item.IconPath = path
		}
	}
	if item.IconPath == "" && item.IconName != "" && filepath.IsAbs(item.IconName) {
		item.IconPath = item.IconName
	}
	if item.Title == "" {
		item.Title = variantString(values["Id"], id)
	}
	s.mu.Lock()
	if _, exists := s.refs[id]; !exists {
		s.mu.Unlock()
		return fmt.Errorf("tray item %q was removed", id)
	}
	s.items[id] = item
	s.mu.Unlock()
	return nil
}

func (s *Service) savePixmapVariant(value dbus.Variant) (string, error) {
	var pixmaps []IconPixmap
	if err := value.Store(&pixmaps); err != nil {
		return "", err
	}
	return s.savePixmap(pixmaps)
}

func (s *Service) savePixmap(pixmaps []IconPixmap) (string, error) {
	if len(pixmaps) == 0 {
		return "", fmt.Errorf("no icon pixmaps")
	}
	sort.SliceStable(pixmaps, func(i, j int) bool {
		return pixmaps[i].Width*pixmaps[i].Height > pixmaps[j].Width*pixmaps[j].Height
	})
	pixmap := pixmaps[0]
	if pixmap.Width < 1 || pixmap.Height < 1 || pixmap.Width > 2048 || pixmap.Height > 2048 || int64(pixmap.Width)*int64(pixmap.Height)*4 > 16*1024*1024 || len(pixmap.Data) < int(pixmap.Width*pixmap.Height*4) {
		return "", fmt.Errorf("invalid icon pixmap dimensions")
	}
	img := image.NewNRGBA(image.Rect(0, 0, int(pixmap.Width), int(pixmap.Height)))
	for y := 0; y < int(pixmap.Height); y++ {
		for x := 0; x < int(pixmap.Width); x++ {
			offset := (y*int(pixmap.Width) + x) * 4
			img.SetNRGBA(x, y, color.NRGBA{A: pixmap.Data[offset], R: pixmap.Data[offset+1], G: pixmap.Data[offset+2], B: pixmap.Data[offset+3]})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		return "", err
	}
	content := buffer.Bytes()
	hash := sha256.Sum256(content)
	if err := os.MkdirAll(s.cacheDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(s.cacheDir, hex.EncodeToString(hash[:])+".png")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Service) menuChildren(children []dbus.Variant) []MenuItem {
	result := make([]MenuItem, 0, len(children))
	for _, childVariant := range children {
		var child menuNode
		if err := childVariant.Store(&child); err != nil {
			continue
		}
		item := menuNodeToItem(child)
		item.Children = s.menuChildren(child.Children)
		if item.Visible {
			result = append(result, item)
		}
	}
	return result
}

func menuNodeToItem(node menuNode) MenuItem {
	item := MenuItem{ID: node.ID, Enabled: true, Visible: true}
	item.Label = variantString(node.Properties["label"], "")
	item.Enabled = variantBoolDefault(node.Properties["enabled"], true)
	item.Visible = variantBoolDefault(node.Properties["visible"], true)
	item.IconName = variantString(node.Properties["icon-name"], "")
	item.IconData, _ = variantBytes(node.Properties["icon-data"])
	item.Type = variantString(node.Properties["type"], "")
	item.ToggleType = variantString(node.Properties["toggle-type"], "")
	item.ToggleState = variantInt32(node.Properties["toggle-state"])
	return item
}

func (s *Service) removeItem(id string, emit bool) {
	s.mu.Lock()
	ref, exists := s.refs[id]
	if !exists {
		s.mu.Unlock()
		return
	}
	delete(s.refs, id)
	delete(s.items, id)
	conn := s.conn
	items := s.registeredItemsLocked()
	s.mu.Unlock()
	if conn != nil {
		_ = conn.RemoveMatchSignal(dbus.WithMatchSender(ref.Owner), dbus.WithMatchObjectPath(ref.Path), dbus.WithMatchInterface(itemInterface))
		if emit {
			_ = conn.Emit(watcherPath, watcherInterface+".StatusNotifierItemUnregistered", itemAddress(ref))
		}
	}
	s.updateWatcherProperties(items)
	if emit {
		s.publish(Event{Type: "removed", ID: id})
		if err := s.persistRefs(); err != nil {
			fmt.Fprintf(os.Stderr, "[tray] persist registrations: %v\n", err)
		}
	}
}

func (s *Service) removeOwner(owner string) {
	s.mu.RLock()
	ids := make([]string, 0)
	for id, ref := range s.refs {
		if ref.Owner == owner {
			ids = append(ids, id)
		}
	}
	s.mu.RUnlock()
	for _, id := range ids {
		s.removeItem(id, true)
	}
	s.mu.Lock()
	delete(s.hosts, owner)
	last := len(s.hosts) == 0
	conn := s.conn
	s.mu.Unlock()
	s.updateHostRegisteredProperty()
	if last && conn != nil {
		_ = conn.Emit(watcherPath, watcherInterface+".StatusNotifierHostUnregistered")
	}
}

func (s *Service) registeredItemsLocked() []string {
	items := make([]string, 0, len(s.refs))
	for _, ref := range s.refs {
		items = append(items, itemAddress(ref))
	}
	sort.Strings(items)
	return items
}

func (s *Service) updateWatcherProperties(items []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if props := s.properties; props != nil && !s.closed {
		props.SetMust(watcherInterface, "RegisteredStatusNotifierItems", items)
	}
}

func (s *Service) updateHostRegisteredProperty() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if props := s.properties; props != nil && !s.closed {
		props.SetMust(watcherInterface, "IsStatusNotifierHostRegistered", true)
	}
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

func itemID(ref ItemRef) string {
	return string(ref.Service) + string(ref.Path)
}

func itemAddress(ref ItemRef) string {
	return ref.Service + string(ref.Path)
}

func dbusNameValid(name string) bool {
	if strings.HasPrefix(name, ":") {
		name = strings.TrimPrefix(name, ":")
	}
	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, char := range part {
			if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-') {
				return false
			}
		}
	}
	return true
}

func variantString(value dbus.Variant, fallback string) string {
	if result, ok := value.Value().(string); ok {
		return result
	}
	return fallback
}

func variantObjectPath(value dbus.Variant) string {
	if result, ok := value.Value().(dbus.ObjectPath); ok {
		return string(result)
	}
	return ""
}

func variantBool(value dbus.Variant) bool {
	result, _ := value.Value().(bool)
	return result
}

func variantBoolDefault(value dbus.Variant, fallback bool) bool {
	result, ok := value.Value().(bool)
	if !ok {
		return fallback
	}
	return result
}

func variantInt32(value dbus.Variant) int32 {
	if result, ok := value.Value().(int32); ok {
		return result
	}
	return 0
}

func variantBytes(value dbus.Variant) (string, error) {
	data, ok := value.Value().([]byte)
	if !ok {
		return "", fmt.Errorf("invalid menu icon data")
	}
	if len(data) == 0 || len(data) > 4*1024*1024 {
		return "", fmt.Errorf("invalid menu icon size")
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
}
