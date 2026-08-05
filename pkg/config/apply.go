package config

import (
	"fmt"

	"axctl/pkg/ipc"
	"axctl/pkg/server"
)

// ApplyConfig generates the static configuration file for the compositor.
// It writes appearance, keybinds, window rules, and layer rules to a static file.
//
// configDir is the directory of the TOML file that declared the configuration.
// It is used to resolve relative paths declared in the [target] section.
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
	return nil
}
