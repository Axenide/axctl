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
	paths      configPaths
}

func NewConfigHandler(c ipc.Compositor) *ConfigHandler {
	return NewConfigHandlerWithOutput(c, "", "")
}

func NewConfigHandlerWithOutput(c ipc.Compositor, primaryPath, altPath string) *ConfigHandler {
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

	paths := PathsForCompositor(c)
	if primaryPath != "" || altPath != "" {
		paths = configPaths{primary: primaryPath, alt: altPath}
	}

	return &ConfigHandler{compositor: c, generator: gen, luaGen: lg, paths: paths}
}

func (h *ConfigHandler) OutputPath() string { return h.paths.primary }

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

	configPath := h.paths.primary
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

	if h.luaGen != nil && h.paths.alt != "" {
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

		luaPath := h.paths.alt
		if err := os.WriteFile(luaPath, []byte(luaConfig.String()), 0644); err != nil {
			return fmt.Errorf("failed to write Lua config to %s: %w", luaPath, err)
		}
		fmt.Printf("Lua config written to: %s\n", luaPath)
	}

	fmt.Printf("Generated Appearance:\n%s\n", appStr)
	fmt.Printf("Generated Keybinds:\n%s\n", bindStr)
	fmt.Printf("Generated Window Rules:\n%s\n", rulesStr)
	fmt.Printf("Generated Layer Rules:\n%s\n", layerStr)
	return h.loadGeneratedConfig(configPath)
}

func (h *ConfigHandler) loadGeneratedConfig(path string) error {
	type loader interface {
		LoadConfig(path string) error
	}
	if l, ok := h.compositor.(loader); ok {
		return l.LoadConfig(path)
	}
	return h.compositor.ReloadConfig()
}
