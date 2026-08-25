package server

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"axctl/pkg/ipc/mock"
)

func TestDarkModeRoundTrip(t *testing.T) {
	original, err := IsDarkMode()
	if err != nil {
		t.Skipf("gsettings unavailable: %v", err)
	}

	if err := SetDarkMode(!original); err != nil {
		t.Fatalf("SetDarkMode(%v) failed: %v", !original, err)
	}
	toggled, err := IsDarkMode()
	if err != nil {
		t.Fatalf("IsDarkMode after toggle failed: %v", err)
	}
	if toggled == original {
		t.Fatalf("expected dark mode to be %v after set, got %v", !original, toggled)
	}

	if err := SetDarkMode(original); err != nil {
		t.Fatalf("restore SetDarkMode(%v) failed: %v", original, err)
	}
	restored, err := IsDarkMode()
	if err != nil {
		t.Fatalf("IsDarkMode after restore failed: %v", err)
	}
	if restored != original {
		t.Fatalf("expected dark mode restored to %v, got %v", original, restored)
	}
}

func TestDarkModeDispatchEndToEnd(t *testing.T) {
	original, err := IsDarkMode()
	if err != nil {
		t.Skipf("gsettings unavailable: %v", err)
	}
	defer func() {
		if setErr := SetDarkMode(original); setErr != nil {
			t.Errorf("failed to restore original dark mode: %v", setErr)
		}
	}()

	socketPath := filepath.Join(t.TempDir(), "axctl-test.sock")
	srv := New(mock.NewCompositor(), socketPath)
	go srv.Start()

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

	for _, method := range []string{"Darkmode.Status", "Darkmode.On", "Darkmode.Off", "Darkmode.Toggle"} {
		conn, dialErr := net.Dial("unix", socketPath)
		if dialErr != nil {
			t.Fatalf("%s: dial failed: %v", method, dialErr)
		}
		req := map[string]interface{}{
			"id":     1,
			"method": method,
			"params": map[string]interface{}{},
		}
		if encErr := json.NewEncoder(conn).Encode(req); encErr != nil {
			t.Fatalf("%s: encode failed: %v", method, encErr)
		}
		var resp struct {
			Result json.RawMessage `json:"result"`
			Error  string          `json:"error"`
		}
		if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
			t.Fatalf("%s: decode failed: %v", method, decErr)
		}
		conn.Close()
		if resp.Error != "" {
			t.Errorf("%s returned error: %s", method, resp.Error)
		}
	}
}
