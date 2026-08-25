package server

import (
	"fmt"
	"os/exec"
	"strings"
)

func preferredColorScheme() (string, error) {
	out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "color-scheme").Output()
	if err != nil {
		return "", fmt.Errorf("gsettings get failed: %v", err)
	}
	return strings.Trim(strings.TrimSpace(string(out)), "'"), nil
}

func IsDarkMode() (bool, error) {
	scheme, err := preferredColorScheme()
	if err != nil {
		return false, err
	}
	return scheme == "prefer-dark", nil
}

func SetDarkMode(on bool) error {
	scheme := "prefer-light"
	if on {
		scheme = "prefer-dark"
	}
	if err := exec.Command("gsettings", "set", "org.gnome.desktop.interface", "color-scheme", scheme).Run(); err != nil {
		return fmt.Errorf("gsettings set failed: %v", err)
	}
	return nil
}
