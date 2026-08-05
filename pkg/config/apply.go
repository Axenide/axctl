package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"axctl/pkg/ipc"
	"axctl/pkg/ipc/hyprland"
	"axctl/pkg/ipc/mango"
	"axctl/pkg/ipc/niri"
	"axctl/pkg/server"
)

// ApplyConfig generates the static configuration files for every compositor
// whose [target] is declared in the TOML. The active compositor is written
// through its live ConfigHandler so it can reload; the inactive ones are
// rendered headlessly via their pure ConfigGenerator so the files are
// always ready, regardless of which compositor is running.
//
// configDir is the directory of the TOML file that declared the
// configuration. It is used to resolve relative paths declared in the
// [target] section.
func ApplyConfig(cfg *TOMLConfig, compositor ipc.Compositor, configDir string) error {
	fmt.Printf("[axctl-config] Generating static config file\n")
	ipcCfg := cfg.ToIPCConfig()

	var hyprlandTarget, niriTarget, mangoTarget string
	if cfg != nil && cfg.Target != nil {
		hyprlandTarget = cfg.Target.Hyprland
		niriTarget = cfg.Target.Niri
		mangoTarget = cfg.Target.Mango
	}
	paths := server.PathsForCompositorWithTarget(compositor, hyprlandTarget, niriTarget, mangoTarget, configDir)

	handler := server.NewConfigHandlerWithOutput(compositor, paths.Primary(), paths.Alt())
	if err := handler.ApplyConfig(ipcCfg); err != nil {
		return fmt.Errorf("failed to apply config: %w", err)
	}

	// Headless pass: render the inactive compositors whose targets are
	// declared. Only the file write — no reload, since the compositor
	// isn't running.
	_, isHypr := compositor.(*hyprland.Hyprland)
	_, isNiri := compositor.(*niri.Niri)
	_, isMango := compositor.(*mango.Mango)

	if !isHypr && hyprlandTarget != "" {
		resolved := server.ResolveTargetPath(hyprlandTarget, configDir)
		primary, alt := splitHyprlandTarget(resolved)
		hyprGen := hyprland.NewGenerator()
		if err := writeConfig(hyprGen, primary, ipcCfg); err != nil {
			return err
		}
		// Hyprland has a Lua side too — render it with the Lua generator.
		// Doing it headlessly keeps the .lua ready without touching Hyprland.
		var luaB strings.Builder
		luaGen := hyprland.NewLuaGenerator()
		luaStartup := luaGen.GenerateStartupLua(ipcCfg.Exec, ipcCfg.ExecOnce)
		if luaStartup != "" {
			luaB.WriteString(luaStartup)
			luaB.WriteString("\n")
		}
		luaB.WriteString(luaGen.GenerateAppearanceLua(ipcCfg.Appearance))
		luaB.WriteString("\n")
		luaB.WriteString(luaGen.GenerateKeybindsLua(ipcCfg.Keybinds))
		luaB.WriteString("\n")
		luaB.WriteString(luaGen.GenerateWindowRulesLua(ipcCfg.WindowRules))
		luaB.WriteString("\n")
		luaB.WriteString(luaGen.GenerateLayerRulesLua(ipcCfg.LayerRules))
		if err := os.WriteFile(alt, []byte(luaB.String()), 0644); err != nil {
			return fmt.Errorf("failed to write Lua config to %s: %w", alt, err)
		}
		fmt.Printf("Lua config written to: %s\n", alt)
	}
	if !isNiri && niriTarget != "" {
		if err := writeConfig(niri.NewGenerator(), server.ResolveTargetPath(niriTarget, configDir), ipcCfg); err != nil {
			return err
		}
	}
	if !isMango && mangoTarget != "" {
		if err := writeConfig(mango.NewGenerator(), server.ResolveTargetPath(mangoTarget, configDir), ipcCfg); err != nil {
			return err
		}
	}

	return nil
}

// writeConfig renders the universal config with the given generator and
// writes it to path. It does NOT reload the compositor — the headless pass
// owns this helper and the inactives aren't running.
func writeConfig(gen ipc.ConfigGenerator, path string, payload ipc.ConfigUniversal) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", filepath.Dir(path), err)
	}
	var b strings.Builder
	startup := gen.GenerateStartup(payload.Exec, payload.ExecOnce)
	if startup != "" {
		b.WriteString(startup)
		b.WriteString("\n")
	}
	b.WriteString(gen.GenerateAppearance(payload.Appearance))
	b.WriteString("\n")
	b.WriteString(gen.GenerateKeybinds(payload.Keybinds))
	b.WriteString("\n")
	b.WriteString(gen.GenerateWindowRules(payload.WindowRules))
	b.WriteString("\n")
	b.WriteString(gen.GenerateLayerRules(payload.LayerRules))
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("failed to write config to %s: %w", path, err)
	}
	fmt.Printf("Config written to: %s\n", path)
	return nil
}

// splitHyprlandTarget mirrors server.splitHyprlandPath's rules so the
// headless pass infers .conf and .lua siblings from a single user target.
func splitHyprlandTarget(resolved string) (string, string) {
	switch {
	case strings.HasSuffix(resolved, ".lua"):
		return strings.TrimSuffix(resolved, ".lua") + ".conf", resolved
	case strings.HasSuffix(resolved, ".conf"):
		return resolved, strings.TrimSuffix(resolved, ".conf") + ".lua"
	default:
		return resolved + ".conf", resolved + ".lua"
	}
}