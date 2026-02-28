package config

import (
	"encoding/json"
	"fmt"

	"axctl/pkg/ipc"
	"axctl/pkg/server"
)

// ApplyConfig applies the TOML configuration to the compositor.
// Each section is applied independently and only if present.
// Order: appearance → keybinds → input → window rules.
func ApplyConfig(cfg *TOMLConfig, compositor ipc.Compositor) error {
	// 1. Appearance → BatchConfig with flat keys
	if cfg.Appearance != nil {
		batch := flattenAppearance(cfg.Appearance)
		if len(batch) > 0 {
			fmt.Printf("[axctl-config] Applying appearance: %d keys\n", len(batch))
			if err := compositor.BatchConfig(batch); err != nil {
				fmt.Printf("[axctl-config] Warning: appearance apply error: %v\n", err)
			}
		}
	}

	// 2. Keybinds → BatchKeybinds as JSON
	if len(cfg.Keybinds) > 0 {
		payload := cfg.ToBatchKeybindsPayload()
		data, err := json.Marshal(payload)
		if err != nil {
			fmt.Printf("[axctl-config] Warning: keybinds marshal error: %v\n", err)
		} else {
			fmt.Printf("[axctl-config] Applying %d keybinds\n", len(payload.Binds))
			if err := compositor.BatchKeybinds(string(data)); err != nil {
				fmt.Printf("[axctl-config] Warning: keybinds apply error: %v\n", err)
			}
		}
	}

	// 3. Input → SetKeyboardLayouts
	if cfg.Input != nil && cfg.Input.Keyboard != nil {
		kb := cfg.Input.Keyboard
		if kb.Layouts != "" {
			fmt.Printf("[axctl-config] Applying keyboard layouts: %s\n", kb.Layouts)
			if err := compositor.SetKeyboardLayouts(kb.Layouts, kb.Variants); err != nil {
				fmt.Printf("[axctl-config] Warning: keyboard layout error: %v\n", err)
			}
		}
	}

	// 4. Window Rules → ConfigHandler.ApplyConfig for static generation
	if len(cfg.WindowRules) > 0 {
		fmt.Printf("[axctl-config] Applying %d window rules\n", len(cfg.WindowRules))
		ipcCfg := cfg.ToIPCConfig()
		rulesCfg := ipc.ConfigUniversal{
			WindowRules: ipcCfg.WindowRules,
		}
		handler := server.NewConfigHandler(compositor)
		if err := handler.ApplyConfig(rulesCfg); err != nil {
			fmt.Printf("[axctl-config] Warning: window rules apply error: %v\n", err)
		}
	}

	return nil
}

// flattenAppearance converts the nested appearance config into a flat key-value map
// suitable for compositor.BatchConfig().
func flattenAppearance(a *AppearanceConfig) map[string]interface{} {
	m := make(map[string]interface{})

	if a.Gaps != nil {
		if a.Gaps.Inner != nil {
			m["gaps.inner"] = *a.Gaps.Inner
		}
		if a.Gaps.Outer != nil {
			m["gaps.outer"] = *a.Gaps.Outer
		}
	}

	if a.Border != nil {
		if a.Border.Width != nil {
			m["border.width"] = *a.Border.Width
		}
		if a.Border.ActiveColor != nil {
			m["border.active_color"] = *a.Border.ActiveColor
		}
		if a.Border.InactiveColor != nil {
			m["border.inactive_color"] = *a.Border.InactiveColor
		}
		if a.Border.Rounding != nil {
			m["border.rounding"] = *a.Border.Rounding
		}
	}

	if a.Opacity != nil {
		if a.Opacity.Active != nil {
			m["opacity.active"] = *a.Opacity.Active
		}
		if a.Opacity.Inactive != nil {
			m["opacity.inactive"] = *a.Opacity.Inactive
		}
	}

	if a.Blur != nil {
		if a.Blur.Enabled != nil {
			m["blur.enabled"] = *a.Blur.Enabled
		}
		if a.Blur.Size != nil {
			m["blur.size"] = *a.Blur.Size
		}
		if a.Blur.Passes != nil {
			m["blur.passes"] = *a.Blur.Passes
		}
	}

	if a.Shadow != nil {
		if a.Shadow.Enabled != nil {
			m["shadow.enabled"] = *a.Shadow.Enabled
		}
		if a.Shadow.Size != nil {
			m["shadow.size"] = *a.Shadow.Size
		}
		if a.Shadow.Color != nil {
			m["shadow.color"] = *a.Shadow.Color
		}
	}

	if a.Animations != nil {
		if a.Animations.Enabled != nil {
			m["animations.enabled"] = *a.Animations.Enabled
		}
	}

	return m
}
