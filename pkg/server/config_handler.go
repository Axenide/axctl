package server

import (
	"fmt"

	"axctl/pkg/ipc"
	"axctl/pkg/ipc/hyprland"
	"axctl/pkg/ipc/niri"
	"axctl/pkg/ipc/mango"
)

type ConfigHandler struct {
	compositor ipc.Compositor
	generator  ipc.ConfigGenerator
}

func NewConfigHandler(c ipc.Compositor) *ConfigHandler {
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
	return &ConfigHandler{compositor: c, generator: gen}
}

func (h *ConfigHandler) ApplyConfig(payload ipc.ConfigUniversal) error {
	if h.generator == nil {
		return fmt.Errorf("ConfigGenerator not supported for this compositor")
	}

	appStr := h.generator.GenerateAppearance(payload.Appearance)
	bindStr := h.generator.GenerateKeybinds(payload.Keybinds)
	rulesStr := h.generator.GenerateWindowRules(payload.WindowRules)

	fmt.Printf("Generated Appearance:\n%s\n", appStr)
	fmt.Printf("Generated Keybinds:\n%s\n", bindStr)
	fmt.Printf("Generated Window Rules:\n%s\n", rulesStr)
	// Write these strings to disk here (e.g. ~/.config/hypr/axctl_managed.conf)

	// Finally trigger a reload
	return h.compositor.ReloadConfig()
}
