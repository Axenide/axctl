package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherAppliesConfigCreatedAfterStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "axctl.toml")
	w, err := NewConfigWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	loaded := make(chan struct{}, 8)
	w.Start(path, func(*TOMLConfig) { loaded <- struct{}{} })

	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "unrelated"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-loaded:
		t.Fatal("unrelated file triggered a reload")
	case <-time.After(400 * time.Millisecond):
	}

	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-loaded:
	case <-time.After(2 * time.Second):
		t.Fatal("config created after Start was not picked up")
	}
}
