package mango

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrNoSignature = errors.New("MANGO_INSTANCE_SIGNATURE not set")

func SessionDetected() bool {
	if os.Getenv("MANGO_INSTANCE_SIGNATURE") != "" {
		return true
	}
	for _, desktop := range strings.Split(os.Getenv("XDG_CURRENT_DESKTOP"), ":") {
		if strings.EqualFold(strings.TrimSpace(desktop), "mango") {
			return true
		}
	}
	return false
}

func resolveSocketPath() (string, error) {
	sig := os.Getenv("MANGO_INSTANCE_SIGNATURE")
	if sig == "" {
		if !SessionDetected() {
			return "", ErrNoSignature
		}
		dir := os.Getenv("XDG_RUNTIME_DIR")
		if dir == "" {
			dir = fmt.Sprintf("/run/user/%d", os.Getuid())
		}
		candidates, err := filepath.Glob(filepath.Join(dir, "mango-*.sock"))
		if err != nil {
			return "", fmt.Errorf("find Mango IPC socket: %w", err)
		}
		sort.Slice(candidates, func(i, j int) bool {
			left, leftErr := os.Stat(candidates[i])
			right, rightErr := os.Stat(candidates[j])
			if leftErr != nil || rightErr != nil {
				return candidates[i] > candidates[j]
			}
			return left.ModTime().After(right.ModTime())
		})
		for _, candidate := range candidates {
			conn, dialErr := net.DialTimeout("unix", candidate, 250*time.Millisecond)
			if dialErr == nil {
				_ = conn.Close()
				return candidate, nil
			}
		}
		return "", fmt.Errorf("%w: no live Mango socket found in %s", ErrNoSignature, dir)
	}
	if filepath.IsAbs(sig) {
		return filepath.Clean(sig), nil
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	return filepath.Join(dir, sig), nil
}
