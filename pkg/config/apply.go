package config

import (
	"fmt"

	"axctl/pkg/ipc"
	"axctl/pkg/server"
)

// ApplyConfig generates the static configuration file for the compositor.
// It writes appearance, keybinds, window rules, and layer rules to a static file.
func ApplyConfig(cfg *TOMLConfig, compositor ipc.Compositor) error {
	fmt.Printf("[axctl-config] Generating static config file\n")
	ipcCfg := cfg.ToIPCConfig()
	handler := server.NewConfigHandler(compositor)
	if err := handler.ApplyConfig(ipcCfg); err != nil {
		return fmt.Errorf("failed to apply config: %w", err)
	}
	return nil
}
