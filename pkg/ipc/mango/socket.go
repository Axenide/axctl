package mango

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrNoSignature = errors.New("MANGO_INSTANCE_SIGNATURE not set")

func resolveSocketPath() (string, error) {
	sig := os.Getenv("MANGO_INSTANCE_SIGNATURE")
	if sig == "" {
		return "", ErrNoSignature
	}
	if len(sig) > 0 && sig[0] == '/' {
		return sig, nil
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	return filepath.Join(dir, "mango-ipc-"+sig), nil
}
