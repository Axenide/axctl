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

func dispatch(t *testing.T, socketPath, method string, params interface{}) (json.RawMessage, string) {
	t.Helper()
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("%s: dial failed: %v", method, err)
	}
	defer conn.Close()
	req := map[string]interface{}{
		"id":     1,
		"method": method,
		"params": params,
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatalf("%s: encode failed: %v", method, err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("%s: decode failed: %v", method, err)
	}
	return resp.Result, resp.Error
}

func TestBrightnessDispatchUnknown(t *testing.T) {
	// Isolate from the host's brightnessctl/ddcutil so the server sees
	// no devices. A nonexistent directory makes exec.LookPath fail
	// without affecting other tests via t.Setenv restoration.
	t.Setenv("PATH", "/nonexistent-axctl-test-path")
	socketPath := filepath.Join(t.TempDir(), "axctl-test.sock")
	srv := New(mock.NewCompositor(), socketPath)
	go srv.Start()
	defer func() {
		// Server doesn't expose Stop; rely on socket cleanup.
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

	// With no devices discovered, Brightness.List must still be wired
	// and return an empty array (not the "method not found" error).
	result, errStr := dispatch(t, socketPath, "Brightness.List", map[string]interface{}{})
	if errStr == "method not found" {
		t.Fatalf("Brightness.List should be wired, got 'method not found'")
	}
	if errStr != "" {
		t.Fatalf("Brightness.List should not error on no devices, got %q", errStr)
	}
	if got := string(result); got != "[]" {
		t.Errorf("Brightness.List with no devices should return [], got %q", got)
	}
}

func TestBrightnessGetRequiresMonitor(t *testing.T) {
	t.Setenv("PATH", "/nonexistent-axctl-test-path")
	socketPath := filepath.Join(t.TempDir(), "axctl-test.sock")
	srv := New(mock.NewCompositor(), socketPath)
	go srv.Start()
	defer os.Remove(socketPath)

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

	_, errStr := dispatch(t, socketPath, "Brightness.Get", map[string]interface{}{})
	if errStr == "" {
		t.Errorf("Brightness.Get without monitor should error")
	}
}
