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
	outputPath string
}

func NewConfigHandler(c ipc.Compositor) *ConfigHandler {
	return NewConfigHandlerWithOutput(c, "")
}

func NewConfigHandlerWithOutput(c ipc.Compositor, outputPath string) *ConfigHandler {
	var gen ipc.ConfigGenerator
	switch c.(type) {
	case *hyprland.Hyprland:
		gen = &hyprland.Generator{}
	case *niri.Niri:
		gen = &niri.Generator{}
	case *mango.Mango:
		gen = &mango.Generator{}
	default:
		gen = nil
	}

	resolvedPath := outputPath
	if resolvedPath == "" {
		resolvedPath = DefaultOutputPath()
	}

	return &ConfigHandler{compositor: c, generator: gen, outputPath: resolvedPath}
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

	// Write to file
	configPath := h.outputPath
	if configPath == "" {
		configPath = DefaultOutputPath()
	}

	// Create parent directories if they don't exist
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write the config file (overwrite existing)
	if err := os.WriteFile(configPath, []byte(fullConfig.String()), 0644); err != nil {
		return fmt.Errorf("failed to write config to %s: %w", configPath, err)
	}

	fmt.Printf("Config written to: %s\n", configPath)
	fmt.Printf("Generated Appearance:\n%s\n", appStr)
	fmt.Printf("Generated Keybinds:\n%s\n", bindStr)
	fmt.Printf("Generated Window Rules:\n%s\n", rulesStr)
	fmt.Printf("Generated Layer Rules:\n%s\n", layerStr)
	// Finally trigger a reload
	return h.compositor.ReloadConfig()
}
