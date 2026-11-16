package notifications

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	root := t.TempDir()
	service := &Service{
		statePath: filepath.Join(root, "state", "notifications.json"),
		imageDir:  filepath.Join(root, "cache", "images"),
		active:    make(map[uint32]Notification),
		timers:    make(map[uint32]*time.Timer),
		listeners: make(map[chan Event]struct{}),
		nextID:    1,
		status:    "stopped",
	}
	return service
}

func TestInternalNotificationReplaceAndActions(t *testing.T) {
	service := newTestService(t)
	events, cancel := service.Subscribe()
	defer cancel()
	firstID, err := service.NotifyInternal(Notification{
		AppName:    "Unit Test",
		Summary:    "First",
		ReplaceKey: "stable-key",
		Actions:    []Action{{ID: "open", Label: "Open"}},
		Timeout:    0,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := service.NotifyInternal(Notification{
		AppName:    "Unit Test",
		Summary:    "Updated",
		ReplaceKey: "stable-key",
		Actions:    []Action{{ID: "open", Label: "Open"}},
		Timeout:    0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if firstID != secondID {
		t.Fatalf("replacement ID = %d, want %d", secondID, firstID)
	}
	if err := service.InvokeAction(firstID, "open"); err != nil {
		t.Fatal(err)
	}
	if got := service.Active(); len(got) != 0 {
		t.Fatalf("active notifications after action = %#v", got)
	}
	history := service.Snapshot()
	if len(history) != 1 || history[0].Summary != "Updated" || history[0].Popup {
		t.Fatalf("history = %#v", history)
	}
	var received, invoked, closed bool
	for range 4 {
		select {
		case event := <-events:
			switch event.Type {
			case "received":
				received = true
			case "action-invoked":
				invoked = true
			case "closed":
				closed = true
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for notification events")
		}
	}
	if !received || !invoked || !closed {
		t.Fatalf("events received=%t invoked=%t closed=%t", received, invoked, closed)
	}
}

func TestNotificationImageDataBoundsAndCache(t *testing.T) {
	service := newTestService(t)
	data := imageData{Width: 2, Height: 1, RowStride: 8, HasAlpha: true, BitsPerSample: 8, Channels: 4, Data: []byte{255, 0, 0, 255, 0, 255, 0, 255}}
	path, err := service.saveImageData(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if _, err := service.saveImageData(imageData{Width: 5000, Height: 1}); err == nil {
		t.Fatal("oversized image should be rejected")
	}
	variant := dbus.MakeVariant(data)
	hints := map[string]dbus.Variant{"image-data": variant}
	fromHints, err := service.imageFromHints(hints)
	if err != nil || fromHints != path {
		t.Fatalf("imageFromHints() = %q, %v; want %q", fromHints, err, path)
	}
}

func TestDNDSuppressesPopupButPreservesHistory(t *testing.T) {
	service := newTestService(t)
	service.SetDoNotDisturb(true)
	if _, err := service.NotifyInternal(Notification{AppName: "test", Summary: "quiet", Timeout: 0, Popup: true}); err != nil {
		t.Fatal(err)
	}
	if got := service.Snapshot(); len(got) != 1 || got[0].Popup {
		t.Fatalf("notification while DND enabled = %#v", got)
	}
}

func TestPauseAndResumeNotificationTimeout(t *testing.T) {
	service := newTestService(t)
	defer service.Close()
	events, cancel := service.Subscribe()
	defer cancel()
	id, err := service.NotifyInternal(Notification{
		AppName: "test",
		Summary: "timed",
		Timeout: 200,
		Popup:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	<-events
	service.Pause(id)
	time.Sleep(250 * time.Millisecond)
	if active := service.Active(); len(active) != 1 {
		t.Fatalf("paused notification expired: %#v", active)
	}
	service.Resume(id)
	deadline := time.After(time.Second)
	for {
		select {
		case event := <-events:
			if event.Type == "closed" {
				if event.Reason != 1 {
					t.Fatalf("close reason = %d, want timeout", event.Reason)
				}
				return
			}
		case <-deadline:
			t.Fatal("resumed notification did not expire")
		}
	}
}

func TestLoadedHistoryDoesNotRestoreActivePopups(t *testing.T) {
	service := newTestService(t)
	service.history = []Notification{{ID: 12, AppName: "test", Popup: true}}
	if err := service.saveLocked(); err != nil {
		t.Fatal(err)
	}
	reloaded := &Service{
		statePath: service.statePath,
		active:    make(map[uint32]Notification),
		timers:    make(map[uint32]*time.Timer),
		listeners: make(map[chan Event]struct{}),
		nextID:    1,
	}
	if err := reloaded.load(); err != nil {
		t.Fatal(err)
	}
	if reloaded.history[0].Popup || reloaded.nextID != 13 {
		t.Fatalf("loaded state = %#v, nextID=%d", reloaded.history, reloaded.nextID)
	}
}

func TestNotificationDBusService(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
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
	signals := make(chan *dbus.Signal, 8)
	client.Signal(signals)
	if err := client.AddMatchSignal(
		dbus.WithMatchObjectPath(objectPath),
		dbus.WithMatchInterface(interfaceID),
		dbus.WithMatchMember("ActionInvoked"),
	); err != nil {
		t.Fatal(err)
	}

	server := client.Object(serviceName, objectPath)
	var capabilities []string
	if err := server.Call(interfaceID+".GetCapabilities", 0).Store(&capabilities); err != nil {
		t.Fatal(err)
	}
	if len(capabilities) == 0 {
		t.Fatal("notification capabilities are empty")
	}
	var id uint32
	if err := server.Call(interfaceID+".Notify", 0,
		"Test", uint32(0), "", "Summary", "Body", []string{"open", "Open"},
		map[string]dbus.Variant{}, int32(0)).Store(&id); err != nil {
		t.Fatal(err)
	}
	if id == 0 || len(service.Active()) != 1 {
		t.Fatalf("notification id=%d active=%#v", id, service.Active())
	}
	if err := service.InvokeAction(id, "open"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(time.Second)
	var actionSignal *dbus.Signal
	for actionSignal == nil {
		select {
		case signal := <-signals:
			if signal.Name == interfaceID+".ActionInvoked" {
				actionSignal = signal
			}
		case <-deadline:
			t.Fatal("timed out waiting for ActionInvoked")
		}
	}
	if actionSignal.Body[0] != id || actionSignal.Body[1] != "open" {
		t.Fatalf("action signal = %#v", actionSignal)
	}
	data, err := json.Marshal(service.Snapshot())
	if err != nil || len(data) == 0 {
		t.Fatalf("notification history serialization failed: %v", err)
	}
}
