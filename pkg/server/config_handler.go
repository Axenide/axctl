package server

import (
	"axctl/pkg/ipc"
	"axctl/pkg/ipc/hyprland"
	"axctl/pkg/ipc/mango"
	"axctl/pkg/ipc/niri"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ConfigHandler struct {
	compositor ipc.Compositor
	generator  ipc.ConfigGenerator
	luaGen     ipc.LuaConfigGenerator
	outputPath string
}

func NewConfigHandler(c ipc.Compositor) *ConfigHandler {
	return NewConfigHandlerWithOutput(c, "")
}

func NewConfigHandlerWithOutput(c ipc.Compositor, outputPath string) *ConfigHandler {
	var gen ipc.ConfigGenerator
	var lg ipc.LuaConfigGenerator
	switch c.(type) {
	case *hyprland.Hyprland:
		gen = &hyprland.Generator{}
		lg = &hyprland.LuaGenerator{}
	case *niri.Niri:
		gen = &niri.Generator{}
	case *mango.Mango:
		gen = &mango.Generator{}
	default:
		gen = nil
	}

	resolvedPath := outputPath
	if resolvedPath == "" {
		resolvedPath = DefaultOutputPathFor(c)
	}

	return &ConfigHandler{compositor: c, generator: gen, luaGen: lg, outputPath: resolvedPath}
}

// DefaultOutputPathFor returns the compositor-specific config output path.
// Hyprland uses a sourced .conf; niri uses a dedicated generated file that is
// included from config.kdl (so we never overwrite the user's config).
func DefaultOutputPathFor(c ipc.Compositor) string {
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		homeDir = "/root"
	}
	switch c.(type) {
	case *niri.Niri:
		return filepath.Join(homeDir, ".config", "niri", "ambxst-generated.kdl")
	default:
		return filepath.Join(homeDir, ".local", "share", "ambxst", "hyprland.conf")
	}
}

func DefaultOutputPath() string {
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		homeDir = "/root"
	}
	return filepath.Join(homeDir, ".local", "share", "ambxst", "hyprland.conf")
}

func (h *ConfigHandler) ApplyConfig(payload ipc.ConfigUniversal) error {
	if h.generator == nil {
		return fmt.Errorf("ConfigGenerator not supported for this compositor")
	}
	startupStr := h.generator.GenerateStartup(payload.Exec, payload.ExecOnce)
	appStr := h.generator.GenerateAppearance(payload.Appearance)
	bindStr := h.generator.GenerateKeybinds(payload.Keybinds)
	rulesStr := h.generator.GenerateWindowRules(payload.WindowRules)
	layerStr := h.generator.GenerateLayerRules(payload.LayerRules)
	if startupStr != "" {
		appStr = strings.TrimPrefix(appStr, "# ▄    ▄▄▄  ▄▄ ▄▄  ▄▄▄▄ ▄▄▄▄▄▄ ▄▄    \n#  ▀▄ ██▀██ ▀█▄█▀ ██▀▀▀   ██   ██    \n# ▄▀  ██▀██ ██ ██ ▀████   ██   ██▄▄▄ \n\n")
	}

	// Combine all generated config
	var fullConfig strings.Builder
	fullConfig.WriteString(startupStr)
	if startupStr != "" {
		fullConfig.WriteString("\n")
	}
	fullConfig.WriteString(appStr)
	fullConfig.WriteString("\n")
	fullConfig.WriteString(bindStr)
	fullConfig.WriteString("\n")
	fullConfig.WriteString(rulesStr)
	fullConfig.WriteString("\n")
	fullConfig.WriteString(layerStr)

	// Write .conf file
	configPath := h.outputPath
	if configPath == "" {
		configPath = DefaultOutputPath()
	}

	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	if err := os.WriteFile(configPath, []byte(fullConfig.String()), 0644); err != nil {
		return fmt.Errorf("failed to write config to %s: %w", configPath, err)
	}
	fmt.Printf("Config written to: %s\n", configPath)

	// For niri, ensure config.kdl includes the generated file so the user's
	// config is never overwritten.
	if _, ok := h.compositor.(*niri.Niri); ok {
		mainPath := filepath.Join(filepath.Dir(configPath), "config.kdl")
		includeLine := `include "` + filepath.Base(configPath) + `"`
		mainData, err := os.ReadFile(mainPath)
		if err != nil {
			// No config.kdl yet — create one with just the include.
			if werr := os.WriteFile(mainPath, []byte(includeLine+"\n"), 0644); werr != nil {
				return werr
			}
		} else if !strings.Contains(string(mainData), includeLine) {
			updated := includeLine + "\n" + string(mainData)
			if werr := os.WriteFile(mainPath, []byte(updated), 0644); werr != nil {
				return werr
			}
		}
	}

	// Write .lua file if Lua generator is available
	if h.luaGen != nil {
		luaStartup := h.luaGen.GenerateStartupLua(payload.Exec, payload.ExecOnce)
		luaApp := h.luaGen.GenerateAppearanceLua(payload.Appearance)
		luaBinds := h.luaGen.GenerateKeybindsLua(payload.Keybinds)
		luaRules := h.luaGen.GenerateWindowRulesLua(payload.WindowRules)
		luaLayers := h.luaGen.GenerateLayerRulesLua(payload.LayerRules)

		var luaConfig strings.Builder
		if luaStartup != "" {
			luaConfig.WriteString(luaStartup)
			luaConfig.WriteString("\n")
		}
		luaConfig.WriteString(luaApp)
		luaConfig.WriteString("\n")
		luaConfig.WriteString(luaBinds)
		luaConfig.WriteString("\n")
		luaConfig.WriteString(luaRules)
		luaConfig.WriteString("\n")
		luaConfig.WriteString(luaLayers)

		luaPath := strings.TrimSuffix(configPath, ".conf") + ".lua"
		if err := os.WriteFile(luaPath, []byte(luaConfig.String()), 0644); err != nil {
			return fmt.Errorf("failed to write Lua config to %s: %w", luaPath, err)
		}
		fmt.Printf("Lua config written to: %s\n", luaPath)
	}

	fmt.Printf("Generated Appearance:\n%s\n", appStr)
	fmt.Printf("Generated Keybinds:\n%s\n", bindStr)
	fmt.Printf("Generated Window Rules:\n%s\n", rulesStr)
	fmt.Printf("Generated Layer Rules:\n%s\n", layerStr)
	// Finally trigger a reload
	return h.compositor.ReloadConfig()
}
