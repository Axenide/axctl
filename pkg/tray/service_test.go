package tray

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

func TestRegisterAddressParsingHelpers(t *testing.T) {
	ref := ItemRef{Service: ":1.25", Owner: ":1.25", Path: dbus.ObjectPath("/StatusNotifierItem")}
	if got := itemID(ref); got != ":1.25/StatusNotifierItem" {
		t.Fatalf("itemID() = %q", got)
	}
	if got := itemAddress(ref); got != ":1.25/StatusNotifierItem" {
		t.Fatalf("itemAddress() = %q", got)
	}
	for _, name := range []string{":1.25", "org.example.Tray", "org.example_tray.Item-1"} {
		if !dbusNameValid(name) {
			t.Errorf("dbusNameValid(%q) = false", name)
		}
	}
	for _, name := range []string{"", "single", "org..tray", "not a bus name"} {
		if dbusNameValid(name) {
			t.Errorf("dbusNameValid(%q) = true", name)
		}
	}
}

func TestMenuLayoutConversion(t *testing.T) {
	child := menuNode{
		ID: 8,
		Properties: map[string]dbus.Variant{
			"label":        dbus.MakeVariant("Enabled"),
			"enabled":      dbus.MakeVariant(true),
			"visible":      dbus.MakeVariant(true),
			"toggle-type":  dbus.MakeVariant("checkmark"),
			"toggle-state": dbus.MakeVariant(int32(1)),
		},
	}
	root := menuNode{Children: []dbus.Variant{dbus.MakeVariant(child)}}
	service := &Service{}
	items := service.menuChildren(root.Children)
	if len(items) != 1 || items[0].ID != 8 || items[0].Label != "Enabled" || items[0].ToggleState != 1 {
		t.Fatalf("menu conversion = %#v", items)
	}
}

func TestSavePixmapPersistsDeterministicImage(t *testing.T) {
	service := &Service{cacheDir: filepath.Join(t.TempDir(), "icons")}
	pixmap := []IconPixmap{{Width: 1, Height: 1, Data: []byte{255, 32, 64, 128}}}
	first, err := service.savePixmap(pixmap)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.savePixmap(pixmap)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("pixmap cache paths differ: %q != %q", first, second)
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(first)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := img.At(0, 0).RGBA()
	if got := [4]uint32{r >> 8, g >> 8, b >> 8, a >> 8}; got != [4]uint32{32, 64, 128, 255} {
		t.Fatalf("decoded pixel = %#v, want ARGB32 [255, 32, 64, 128] converted to RGBA [32, 64, 128, 255]", got)
	}
}

type fakeStatusNotifierItem struct {
	activated atomic.Int32
}

func (f *fakeStatusNotifierItem) Activate(_ int32, _ int32) *dbus.Error {
	f.activated.Add(1)
	return nil
}

type fakeStatusNotifierWatcher struct {
	items       []string
	rejectHosts bool
	hosts       atomic.Int32
}

func (w *fakeStatusNotifierWatcher) RegisterStatusNotifierItem(_ dbus.Sender, _ string) *dbus.Error {
	return nil
}

func (w *fakeStatusNotifierWatcher) RegisterStatusNotifierHost(_ dbus.Sender, _ string) *dbus.Error {
	w.hosts.Add(1)
	if w.rejectHosts {
		return dbus.MakeFailedError(fmt.Errorf("additional hosts are not supported"))
	}
	return nil
}

func (w *fakeStatusNotifierWatcher) IsStatusNotifierHostRegistered() (bool, *dbus.Error) {
	return true, nil
}

func (w *fakeStatusNotifierWatcher) RegisteredStatusNotifierItems() ([]string, *dbus.Error) {
	return w.items, nil
}

func (w *fakeStatusNotifierWatcher) ProtocolVersion() (int32, *dbus.Error) {
	return 0, nil
}

func findItemByTitle(items []Item, title string) (Item, bool) {
	for _, item := range items {
		if item.Title == title {
			return item, true
		}
	}
	return Item{}, false
}

func waitForItemByTitle(t *testing.T, service *Service, title string) Item {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for {
		if item, ok := findItemByTitle(service.List(), title); ok {
			return item
		}
		select {
		case <-ticker.C:
		case <-timeout.C:
			t.Fatalf("tray item %q was not loaded", title)
		}
	}
}

func waitForItemRemoval(t *testing.T, service *Service, id string) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for {
		found := false
		for _, item := range service.List() {
			if item.ID == id {
				found = true
				break
			}
		}
		if !found {
			return
		}
		select {
		case <-ticker.C:
		case <-timeout.C:
			t.Fatalf("tray item %q was not removed", id)
		}
	}
}

func waitForWatcherOwner(t *testing.T, conn *dbus.Conn, want string) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for {
		call := conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, watcherName)
		var owner string
		if call.Err == nil && call.Store(&owner) == nil && owner == want {
			return
		}
		select {
		case <-ticker.C:
		case <-timeout.C:
			t.Fatalf("StatusNotifier watcher owner did not become %q", want)
		}
	}
}

func exportTestStatusNotifierItem(t *testing.T, conn *dbus.Conn, path dbus.ObjectPath, title string) {
	t.Helper()
	if err := conn.Export(&fakeStatusNotifierItem{}, path, itemInterface); err != nil {
		t.Fatal(err)
	}
	_, err := prop.Export(conn, path, prop.Map{itemInterface: {
		"Category": &prop.Prop{Value: "ApplicationStatus", Emit: prop.EmitTrue},
		"Id":       &prop.Prop{Value: title, Emit: prop.EmitTrue},
		"Title":    &prop.Prop{Value: title, Emit: prop.EmitTrue},
		"Status":   &prop.Prop{Value: "Active", Emit: prop.EmitTrue},
		"IconName": &prop.Prop{Value: "test-icon", Emit: prop.EmitTrue},
	}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRescanFindsItemsOnNonstandardPaths(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	itemConn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Skipf("session D-Bus is unavailable: %v", err)
	}
	defer itemConn.Close()
	itemPath := dbus.ObjectPath("/com/example/tray/StatusNotifierItem")
	exportTestStatusNotifierItem(t, itemConn, itemPath, "Nonstandard Rescan Item")
	introspection := []struct {
		path dbus.ObjectPath
		node *introspect.Node
	}{
		{path: "/", node: &introspect.Node{Name: "/", Children: []introspect.Node{{Name: "com"}}}},
		{path: "/com", node: &introspect.Node{Name: "/com", Children: []introspect.Node{{Name: "example"}}}},
		{path: "/com/example", node: &introspect.Node{Name: "/com/example", Children: []introspect.Node{{Name: "tray"}}}},
		{path: "/com/example/tray", node: &introspect.Node{Name: "/com/example/tray", Children: []introspect.Node{{Name: "StatusNotifierItem"}}}},
		{path: itemPath, node: &introspect.Node{Name: string(itemPath), Interfaces: []introspect.Interface{{Name: itemInterface}}}},
	}
	for _, entry := range introspection {
		if err := itemConn.Export(introspect.NewIntrospectable(entry.node), entry.path, "org.freedesktop.DBus.Introspectable"); err != nil {
			t.Fatal(err)
		}
	}

	scanConn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	service, err := New()
	if err != nil {
		scanConn.Close()
		t.Fatal(err)
	}
	service.conn = scanConn
	defer service.Close()
	registered, err := service.Rescan()
	if err != nil {
		t.Fatal(err)
	}
	if registered < 1 {
		t.Fatalf("Rescan registered %d items, want at least one", registered)
	}
	item := waitForItemByTitle(t, service, "Nonstandard Rescan Item")
	if item.ID != string(itemConn.Names()[0])+string(itemPath) {
		t.Fatalf("rescanned item ID = %q, want %q", item.ID, string(itemConn.Names()[0])+string(itemPath))
	}
}

func TestStartRescansUnregisteredItem(t *testing.T) {
	t.Setenv("AXCTL_TRAY", "1")
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	itemConn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Skipf("session D-Bus is unavailable: %v", err)
	}
	defer itemConn.Close()
	itemPath := dbus.ObjectPath("/StatusNotifierItem")
	exportTestStatusNotifierItem(t, itemConn, itemPath, "Startup Rescan Item")

	service, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Start(); err != nil {
		t.Skipf("tray service is unavailable: %v", err)
	}
	defer service.Close()
	item := waitForItemByTitle(t, service, "Startup Rescan Item")
	if item.ID != string(itemConn.Names()[0])+string(itemPath) {
		t.Fatalf("rescanned item ID = %q, want %q", item.ID, string(itemConn.Names()[0])+string(itemPath))
	}
}

func TestStatusNotifierWatcherAttachesToExistingHost(t *testing.T) {
	t.Setenv("AXCTL_TRAY", "1")
	tempDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(tempDir, "runtime"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tempDir, "cache"))

	watcherConn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Skipf("session D-Bus is unavailable: %v", err)
	}
	defer watcherConn.Close()
	itemConn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer itemConn.Close()
	itemPath := dbus.ObjectPath("/StatusNotifierItem")
	if err := itemConn.Export(&fakeStatusNotifierItem{}, itemPath, itemInterface); err != nil {
		t.Fatal(err)
	}
	_, err = prop.Export(itemConn, itemPath, prop.Map{itemInterface: {
		"Category": &prop.Prop{Value: "ApplicationStatus", Emit: prop.EmitTrue},
		"Id":       &prop.Prop{Value: "external-item", Emit: prop.EmitTrue},
		"Title":    &prop.Prop{Value: "External Item", Emit: prop.EmitTrue},
		"Status":   &prop.Prop{Value: "Active", Emit: prop.EmitTrue},
		"IconName": &prop.Prop{Value: "external-icon", Emit: prop.EmitTrue},
	}})
	if err != nil {
		t.Fatal(err)
	}
	itemAddress := string(itemConn.Names()[0]) + string(itemPath)
	watcher := &fakeStatusNotifierWatcher{items: []string{itemAddress}, rejectHosts: true}
	if err := watcherConn.Export(watcher, watcherPath, watcherInterface); err != nil {
		t.Fatal(err)
	}
	_, err = prop.Export(watcherConn, watcherPath, prop.Map{watcherInterface: {
		"RegisteredStatusNotifierItems":  &prop.Prop{Value: watcher.items, Emit: prop.EmitTrue},
		"IsStatusNotifierHostRegistered": &prop.Prop{Value: true, Emit: prop.EmitTrue},
		"ProtocolVersion":                &prop.Prop{Value: int32(0), Emit: prop.EmitConst},
	}})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := watcherConn.RequestName(watcherName, dbus.NameFlagDoNotQueue)
	if err != nil {
		t.Fatal(err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		t.Skipf("another StatusNotifier watcher owns %s", watcherName)
	}

	service, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Start(); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if service.Status() != "available" {
		t.Fatalf("tray status = %q, want available", service.Status())
	}
	if watcher.hosts.Load() != 1 {
		t.Fatalf("existing watcher received %d host registrations, want 1", watcher.hosts.Load())
	}
	item := waitForItemByTitle(t, service, "External Item")
	if item.Service != string(itemConn.Names()[0]) {
		t.Fatalf("item service = %q, want %q", item.Service, itemConn.Names()[0])
	}

	livePath := dbus.ObjectPath("/LiveStatusNotifierItem")
	if err := itemConn.Export(&fakeStatusNotifierItem{}, livePath, itemInterface); err != nil {
		t.Fatal(err)
	}
	_, err = prop.Export(itemConn, livePath, prop.Map{itemInterface: {
		"Category": &prop.Prop{Value: "ApplicationStatus", Emit: prop.EmitTrue},
		"Id":       &prop.Prop{Value: "live-item", Emit: prop.EmitTrue},
		"Title":    &prop.Prop{Value: "Live Item", Emit: prop.EmitTrue},
		"Status":   &prop.Prop{Value: "Active", Emit: prop.EmitTrue},
		"IconName": &prop.Prop{Value: "live-icon", Emit: prop.EmitTrue},
	}})
	if err != nil {
		t.Fatal(err)
	}
	liveAddress := string(itemConn.Names()[0]) + string(livePath)
	if err := watcherConn.Emit(watcherPath, watcherInterface+".StatusNotifierItemRegistered", liveAddress); err != nil {
		t.Fatal(err)
	}
	liveItem := waitForItemByTitle(t, service, "Live Item")
	if err := watcherConn.Emit(watcherPath, watcherInterface+".StatusNotifierItemUnregistered", itemAddress); err != nil {
		t.Fatal(err)
	}
	waitForItemRemoval(t, service, item.ID)

	if err := watcherConn.Close(); err != nil {
		t.Fatal(err)
	}
	service.mu.RLock()
	ownName := string(service.conn.Names()[0])
	service.mu.RUnlock()
	waitForWatcherOwner(t, itemConn, ownName)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for {
		service.mu.RLock()
		ownsWatcher := service.ownsWatcher
		service.mu.RUnlock()
		if ownsWatcher {
			break
		}
		select {
		case <-ticker.C:
		case <-timeout.C:
			t.Fatal("axctl did not take over the watcher name")
		}
	}
	if item, ok := findItemByTitle(service.List(), "Live Item"); !ok || item.ID != liveItem.ID {
		t.Fatalf("existing item after watcher takeover = %#v", service.List())
	}
}

func TestStatusNotifierWatcherReadyBeforeNameOwnership(t *testing.T) {
	t.Setenv("AXCTL_TRAY", "1")
	tempDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(tempDir, "runtime"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tempDir, "cache"))
	service, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	client, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Skipf("session D-Bus is unavailable: %v", err)
	}
	defer client.Close()
	itemPath := dbus.ObjectPath("/StatusNotifierItem")
	if err := client.Export(&fakeStatusNotifierItem{}, itemPath, itemInterface); err != nil {
		t.Fatal(err)
	}
	_, err = prop.Export(client, itemPath, prop.Map{itemInterface: {
		"Category": &prop.Prop{Value: "ApplicationStatus", Emit: prop.EmitTrue},
		"Id":       &prop.Prop{Value: "early-item", Emit: prop.EmitTrue},
		"Title":    &prop.Prop{Value: "Early Item", Emit: prop.EmitTrue},
		"Status":   &prop.Prop{Value: "Active", Emit: prop.EmitTrue},
		"IconName": &prop.Prop{Value: "early-icon", Emit: prop.EmitTrue},
	}})
	if err != nil {
		t.Fatal(err)
	}

	signals := make(chan *dbus.Signal, 8)
	client.Signal(signals)
	if err := client.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
	); err != nil {
		t.Fatal(err)
	}
	defer client.RemoveSignal(signals)
	startDone := make(chan error, 1)
	go func() { startDone <- service.Start() }()

	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	started := false
waitForOwner:
	for {
		select {
		case signal := <-signals:
			if signal == nil || signal.Name != "org.freedesktop.DBus.NameOwnerChanged" || len(signal.Body) < 3 {
				continue
			}
			name, _ := signal.Body[0].(string)
			newOwner, _ := signal.Body[2].(string)
			if name == watcherName && newOwner != "" {
				break waitForOwner
			}
		case err := <-startDone:
			if err != nil {
				t.Skipf("StatusNotifier watcher is unavailable: %v", err)
			}
			startDone = nil
			started = true
		case <-deadline.C:
			t.Fatal("timed out waiting for the watcher to acquire its D-Bus name")
		}
	}

	var hostRegistered dbus.Variant
	call := client.Object(watcherName, watcherPath).Call("org.freedesktop.DBus.Properties.Get", 0, watcherInterface, "IsStatusNotifierHostRegistered")
	if call.Err != nil || call.Store(&hostRegistered) != nil {
		t.Fatalf("read host registration property: %v", call.Err)
	}
	if got, ok := hostRegistered.Value().(bool); !ok || !got {
		t.Fatalf("IsStatusNotifierHostRegistered = %v, want true", hostRegistered.Value())
	}
	if call := client.Object(watcherName, watcherPath).Call(watcherInterface+".RegisterStatusNotifierItem", 0, string(itemPath)); call.Err != nil {
		t.Fatalf("register item immediately after watcher startup: %v", call.Err)
	}
	missingPath := dbus.ObjectPath("/MissingStatusNotifierItem")
	if call := client.Object(watcherName, watcherPath).Call(watcherInterface+".RegisterStatusNotifierItem", 0, string(missingPath)); call.Err != nil {
		t.Fatalf("accept item before its properties are available: %v", call.Err)
	}
	registeredItems, dbusErr := service.RegisteredStatusNotifierItems()
	if dbusErr != nil {
		t.Fatal(dbusErr)
	}
	registered := false
	for _, address := range registeredItems {
		if address == string(client.Names()[0])+string(missingPath) {
			registered = true
			break
		}
	}
	if !registered {
		t.Fatalf("missing item was not retained in watcher registrations: %#v", registeredItems)
	}
	if !started {
		select {
		case err := <-startDone:
			if err != nil {
				t.Fatal(err)
			}
		case <-deadline.C:
			t.Fatal("timed out waiting for tray service startup")
		}
	}
	waitForItemByTitle(t, service, "Early Item")
}

type fakeDBusMenu struct {
	events          atomic.Int32
	failAboutToShow atomic.Bool
}

func (m *fakeDBusMenu) AboutToShow(_ int32) (bool, *dbus.Error) {
	if m.failAboutToShow.Load() {
		return false, dbus.MakeFailedError(fmt.Errorf("AboutToShow failed"))
	}
	return false, nil
}

func (m *fakeDBusMenu) GetLayout(_ int32, _ int32, _ []string) (uint32, menuNode, *dbus.Error) {
	return 1, menuNode{
		ID: 0,
		Children: []dbus.Variant{dbus.MakeVariant(menuNode{
			ID: 8,
			Properties: map[string]dbus.Variant{
				"label":   dbus.MakeVariant("Open"),
				"enabled": dbus.MakeVariant(true),
				"visible": dbus.MakeVariant(true),
			},
		})},
	}, nil
}

func (m *fakeDBusMenu) Event(_ int32, _ string, _ dbus.Variant, _ uint32) *dbus.Error {
	m.events.Add(1)
	return nil
}

func TestStatusNotifierWatcherRegistration(t *testing.T) {
	t.Setenv("AXCTL_TRAY", "1")
	tempDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(tempDir, "runtime"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tempDir, "cache"))
	service, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Start(); err != nil {
		t.Skipf("session D-Bus is unavailable: %v", err)
	}
	defer service.Close()

	client, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	itemPath := dbus.ObjectPath("/StatusNotifierItem")
	fake := &fakeStatusNotifierItem{}
	if err := client.Export(fake, itemPath, itemInterface); err != nil {
		t.Fatal(err)
	}
	_, err = prop.Export(client, itemPath, prop.Map{itemInterface: {
		"Category":   &prop.Prop{Value: "ApplicationStatus", Emit: prop.EmitTrue},
		"Id":         &prop.Prop{Value: "fake-item", Emit: prop.EmitTrue},
		"Title":      &prop.Prop{Value: "Fake Item", Emit: prop.EmitTrue},
		"Status":     &prop.Prop{Value: "Active", Emit: prop.EmitTrue},
		"IconName":   &prop.Prop{Value: "fake-icon", Emit: prop.EmitTrue},
		"Menu":       &prop.Prop{Value: dbus.ObjectPath("/Menu"), Emit: prop.EmitTrue},
		"ItemIsMenu": &prop.Prop{Value: false, Emit: prop.EmitTrue},
	}})
	if err != nil {
		t.Fatal(err)
	}
	menuPath := dbus.ObjectPath("/Menu")
	fakeMenu := &fakeDBusMenu{}
	fakeMenu.failAboutToShow.Store(true)
	if err := client.Export(fakeMenu, menuPath, menuInterface); err != nil {
		t.Fatal(err)
	}
	watcher := client.Object(watcherName, watcherPath)
	if err := watcher.Call(watcherInterface+".RegisterStatusNotifierItem", 0, string(itemPath)).Err; err != nil {
		t.Fatal(err)
	}
	item := waitForItemByTitle(t, service, "Fake Item")
	if item.IconName != "fake-icon" {
		t.Fatalf("registered item = %#v", item)
	}
	if err := service.Activate(item.ID, 0, 0); err != nil {
		t.Fatal(err)
	}
	if fake.activated.Load() != 1 {
		t.Fatalf("Activate called %d times", fake.activated.Load())
	}
	_, menuItems, err := service.Menu(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(menuItems) != 1 || menuItems[0].Label != "Open" {
		t.Fatalf("menu items = %#v", menuItems)
	}
	if err := service.MenuEvent(item.ID, menuItems[0].ID, "clicked"); err != nil {
		t.Fatal(err)
	}
	if fakeMenu.events.Load() != 1 {
		t.Fatalf("menu events = %d", fakeMenu.events.Load())
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	restored := waitForItemByTitle(t, restarted, "Fake Item")
	if restored.ID != item.ID {
		t.Fatalf("restored item = %#v, want ID %q", restored, item.ID)
	}
}
