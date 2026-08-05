package server

import (
	"os"
	"path/filepath"

	"axctl/pkg/ipc/hyprland"
	"axctl/pkg/ipc/mango"
	"axctl/pkg/ipc/niri"
)

type configPaths struct {
	primary string
	alt     string
}

func xdgConfigDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config")
	}
	return "/tmp"
}

func legacyHyprlandPath() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = "/root"
	}
	return filepath.Join(home, ".local", "share", "ambxst", "hyprland.conf")
}

func pathsFor(primary string) configPaths {
	return configPaths{primary: primary}
}

func DefaultOutputPath() string {
	return legacyHyprlandPath()
}

func PathsForCompositor(c interface{}) configPaths {
	dir := xdgConfigDir()
	switch c.(type) {
	case *hyprland.Hyprland:
		return configPaths{
			primary: filepath.Join(dir, "hypr", "axctl.generated.conf"),
			alt:     filepath.Join(dir, "hypr", "axctl.generated.lua"),
		}
	case *niri.Niri:
		return configPaths{
			primary: filepath.Join(dir, "niri", "axctl.generated.kdl"),
		}
	case *mango.Mango:
		return configPaths{
			primary: filepath.Join(dir, "mango", "axctl.generated.conf"),
		}
	default:
		return pathsFor(legacyHyprlandPath())
	}
}
