package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSocketPathPrefersRuntimeDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AXCTL_SOCKET", "")
	t.Setenv("XDG_RUNTIME_DIR", dir)
	if got := defaultSocketPath(); got != filepath.Join(dir, "axctl.sock") {
		t.Fatalf("want runtime-dir path, got %q", got)
	}
}

func TestDefaultSocketPathFallsBackToTmp(t *testing.T) {
	t.Setenv("AXCTL_SOCKET", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	want := fmt.Sprintf("/tmp/axctl-%d.sock", os.Getuid())
	if got := defaultSocketPath(); got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestDefaultSocketPathOverride(t *testing.T) {
	t.Setenv("AXCTL_SOCKET", "/custom/axctl.sock")
	if got := defaultSocketPath(); got != "/custom/axctl.sock" {
		t.Fatalf("want override honored, got %q", got)
	}
}
