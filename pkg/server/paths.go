package server

import (
	"os"
	"path/filepath"
	"strings"

	"axctl/pkg/ipc/hyprland"
	"axctl/pkg/ipc/mango"
	"axctl/pkg/ipc/niri"
)

type configPaths struct {
	primary string
	alt     string
}

func (p configPaths) Primary() string { return p.primary }
func (p configPaths) Alt() string     { return p.alt }

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

// ResolveTargetPath expands a target path from a [target] TOML section.
// "~" and "~/..." expand against $HOME. Absolute paths pass through.
// Anything else is treated as relative to configDir (the TOML's directory).
func ResolveTargetPath(target, configDir string) string {
	if target == "" {
		return ""
	}
	if home, _ := os.UserHomeDir(); home != "" {
		if target == "~" {
			return home
		}
		if strings.HasPrefix(target, "~/") {
			return filepath.Join(home, target[2:])
		}
	}
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(configDir, target)
}

// PathsForCompositorWithTarget returns the output paths for c, honoring
// the optional [target] section of the TOML config. When the relevant
// target field is empty, the compositor default paths are used.
//
// configDir is the directory of the TOML file that declared the [target]
// section, used to resolve relative target paths.
//
// The three target strings are passed as separate arguments to avoid an
// import cycle with the config package, which already imports server.
func PathsForCompositorWithTarget(c interface{}, hyprlandTarget, niriTarget, mangoTarget, configDir string) configPaths {
	var override string
	switch c.(type) {
	case *hyprland.Hyprland:
		override = hyprlandTarget
	case *niri.Niri:
		override = niriTarget
	case *mango.Mango:
		override = mangoTarget
	}
	if override == "" {
		return PathsForCompositor(c)
	}
	resolved := ResolveTargetPath(override, configDir)
	if _, ok := c.(*hyprland.Hyprland); ok {
		return splitHyprlandPath(resolved)
	}
	return configPaths{primary: resolved}
}

// splitHyprlandPath maps a single user-supplied hyprland target to both
// the .conf and .lua output paths. The format is inferred from the
// extension; if the user gave no extension, ".conf" and ".lua" are
// appended to produce sibling files.
func splitHyprlandPath(resolved string) configPaths {
	switch {
	case strings.HasSuffix(resolved, ".lua"):
		return configPaths{
			primary: strings.TrimSuffix(resolved, ".lua") + ".conf",
			alt:     resolved,
		}
	case strings.HasSuffix(resolved, ".conf"):
		return configPaths{
			primary: resolved,
			alt:     strings.TrimSuffix(resolved, ".conf") + ".lua",
		}
	default:
		return configPaths{
			primary: resolved + ".conf",
			alt:     resolved + ".lua",
		}
	}
}
