package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"axctl/pkg/ipc"
	"axctl/pkg/ipc/mock"
	"axctl/pkg/keymon"
)

type recordingCompositor struct {
	*mock.Compositor

	batchKeybindsPayloads []string
	bindKeyCalls          []string
	unbindKeyCalls        []string
}

func (c *recordingCompositor) BatchKeybinds(jsonPayload string) error {
	c.batchKeybindsPayloads = append(c.batchKeybindsPayloads, jsonPayload)
	return nil
}

func (c *recordingCompositor) BindKey(mods, key, command string) error {
	c.bindKeyCalls = append(c.bindKeyCalls, mods+","+key+","+command)
	return nil
}

func (c *recordingCompositor) UnbindKey(mods, key string) error {
	c.unbindKeyCalls = append(c.unbindKeyCalls, mods+","+key)
	return nil
}

func modifierSelfPayload() ipc.ConfigUniversal {
	return ipc.ConfigUniversal{
		Keybinds: ipc.ConfigKeybinds{
			Custom: []ipc.Keybind{
				{Modifiers: []string{"SUPER"}, Key: "Super_L", Dispatcher: "exec", Argument: "ambxst run launcher", Enabled: true},
			},
		},
	}
}

func TestSyncKeyMonitorOnlyActiveOnNiri(t *testing.T) {
	srv := &Server{
		compositor: mock.NewCompositor(),
		cfgState:   NewConfigState(),
		keyMon:     keymon.NewMonitor(),
	}

	srv.SeedConfigState(modifierSelfPayload())

	status := srv.keyMon.Status()
	if status.Open {
		t.Fatalf("keymon must not open devices on non-niri compositors")
	}
	if len(status.Binds) != 0 {
		t.Fatalf("keymon binds must be empty on non-niri compositors, got %v", status.Binds)
	}
}

func TestBindKeyModifierSelfRoutesToBatchOnNonNiri(t *testing.T) {
	rec := &recordingCompositor{Compositor: mock.NewCompositor()}
	socketPath := filepath.Join(t.TempDir(), "axctl-test.sock")
	srv := New(rec, socketPath)
	go srv.Start()
	defer func() {
		_ = os.Remove(socketPath)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, statErr := os.Stat(socketPath); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("test server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	_, errStr := dispatch(t, socketPath, "Config.BindKey", map[string]interface{}{
		"mods":    "SUPER",
		"key":     "Super_L",
		"command": "ambxst run launcher",
	})
	if errStr != "" {
		t.Fatalf("Config.BindKey error = %q", errStr)
	}

	if len(rec.bindKeyCalls) != 0 {
		t.Fatalf("modifier-self bind must not go through BindKey, got %v", rec.bindKeyCalls)
	}
	if len(rec.batchKeybindsPayloads) != 1 {
		t.Fatalf("modifier-self bind should be routed through BatchKeybinds, got %d calls", len(rec.batchKeybindsPayloads))
	}

	var payload ipc.BatchKeybindsPayload
	if err := json.Unmarshal([]byte(rec.batchKeybindsPayloads[0]), &payload); err != nil {
		t.Fatalf("batch payload unmarshal: %v", err)
	}
	if len(payload.Binds) != 1 || payload.Binds[0].Key != "Super_L" || !payload.Binds[0].Enabled {
		t.Fatalf("batch payload should carry the Super_L bind, got %+v", payload)
	}

	status := srv.keyMon.Status()
	if status.Open || len(status.Binds) != 0 {
		t.Fatalf("keymon must stay inactive on non-niri compositors, got %+v", status)
	}
}

func TestUnbindKeyModifierSelfRoutesToCompositorOnNonNiri(t *testing.T) {
	rec := &recordingCompositor{Compositor: mock.NewCompositor()}
	socketPath := filepath.Join(t.TempDir(), "axctl-test.sock")
	srv := New(rec, socketPath)
	go srv.Start()
	defer func() {
		_ = os.Remove(socketPath)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, statErr := os.Stat(socketPath); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("test server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	_, errStr := dispatch(t, socketPath, "Config.UnbindKey", map[string]interface{}{
		"mods": "SUPER",
		"key":  "Super_L",
	})
	if errStr != "" {
		t.Fatalf("Config.UnbindKey error = %q", errStr)
	}

	found := false
	for _, call := range rec.unbindKeyCalls {
		if strings.HasPrefix(call, "SUPER,Super_L") {
			found = true
		}
	}
	if !found {
		t.Fatalf("modifier-self unbind should reach the compositor, got %v", rec.unbindKeyCalls)
	}

	status := srv.keyMon.Status()
	if status.Open || len(status.Binds) != 0 {
		t.Fatalf("keymon must stay inactive on non-niri compositors, got %+v", status)
	}
}
