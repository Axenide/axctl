package tray

import (
	"image/png"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/godbus/dbus/v5"
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

type fakeDBusMenu struct {
	events atomic.Int32
}

func (m *fakeDBusMenu) AboutToShow(_ int32) (bool, *dbus.Error) {
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
	if err := client.Export(fakeMenu, menuPath, menuInterface); err != nil {
		t.Fatal(err)
	}
	watcher := client.Object(watcherName, watcherPath)
	if err := watcher.Call(watcherInterface+".RegisterStatusNotifierItem", 0, string(itemPath)).Err; err != nil {
		t.Fatal(err)
	}
	items := service.List()
	if len(items) != 1 || items[0].Title != "Fake Item" || items[0].IconName != "fake-icon" {
		t.Fatalf("registered items = %#v", items)
	}
	if err := service.Activate(items[0].ID, 0, 0); err != nil {
		t.Fatal(err)
	}
	if fake.activated.Load() != 1 {
		t.Fatalf("Activate called %d times", fake.activated.Load())
	}
	_, menuItems, err := service.Menu(items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(menuItems) != 1 || menuItems[0].Label != "Open" {
		t.Fatalf("menu items = %#v", menuItems)
	}
	if err := service.MenuEvent(items[0].ID, menuItems[0].ID, "clicked"); err != nil {
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
	restoredItems := restarted.List()
	if len(restoredItems) != 1 || restoredItems[0].ID != items[0].ID || restoredItems[0].Title != "Fake Item" {
		t.Fatalf("restored items = %#v", restoredItems)
	}
}
