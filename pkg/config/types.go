package config

import "axctl/pkg/ipc"

// TOMLConfig is the top-level configuration structure parsed from TOML files.
type TOMLConfig struct {
	Include     []string           `toml:"include,omitempty"`
	Appearance  *AppearanceConfig  `toml:"appearance,omitempty"`
	Input       *InputConfig       `toml:"input,omitempty"`
	Keybinds    []KeybindConfig    `toml:"keybinds,omitempty"`
	WindowRules []WindowRuleConfig `toml:"window_rules,omitempty"`
}

// AppearanceConfig mirrors ipc.ConfigAppearance with TOML tags.
type AppearanceConfig struct {
	Gaps       *GapsConfig       `toml:"gaps,omitempty"`
	Border     *BorderConfig     `toml:"border,omitempty"`
	Opacity    *OpacityConfig    `toml:"opacity,omitempty"`
	Blur       *BlurConfig       `toml:"blur,omitempty"`
	Shadow     *ShadowConfig     `toml:"shadow,omitempty"`
	Animations *AnimationsConfig `toml:"animations,omitempty"`
}

// GapsConfig mirrors ipc.Gaps with TOML tags.
type GapsConfig struct {
	Inner *int `toml:"inner,omitempty"`
	Outer *int `toml:"outer,omitempty"`
}

// BorderConfig mirrors ipc.Border with TOML tags.
type BorderConfig struct {
	Width         *int    `toml:"width,omitempty"`
	ActiveColor   *string `toml:"active_color,omitempty"`
	InactiveColor *string `toml:"inactive_color,omitempty"`
	Rounding      *int    `toml:"rounding,omitempty"`
}

// OpacityConfig mirrors ipc.Opacity with TOML tags.
type OpacityConfig struct {
	Active   *float64 `toml:"active,omitempty"`
	Inactive *float64 `toml:"inactive,omitempty"`
}

// BlurConfig mirrors ipc.Blur with TOML tags.
type BlurConfig struct {
	Enabled *bool `toml:"enabled,omitempty"`
	Size    *int  `toml:"size,omitempty"`
	Passes  *int  `toml:"passes,omitempty"`
}

// ShadowConfig mirrors ipc.Shadow with TOML tags.
type ShadowConfig struct {
	Enabled *bool   `toml:"enabled,omitempty"`
	Size    *int    `toml:"size,omitempty"`
	Color   *string `toml:"color,omitempty"`
}

// AnimationsConfig mirrors ipc.Animations with TOML tags.
type AnimationsConfig struct {
	Enabled *bool `toml:"enabled,omitempty"`
}

// InputConfig holds input-related configuration.
type InputConfig struct {
	Keyboard *KeyboardConfig `toml:"keyboard,omitempty"`
}

// KeyboardConfig holds keyboard layout configuration.
type KeyboardConfig struct {
	Layouts  string `toml:"layouts,omitempty"`
	Variants string `toml:"variants,omitempty"`
}

// KeybindConfig mirrors ipc.Keybind with TOML tags.
type KeybindConfig struct {
	Modifiers  []string `toml:"modifiers"`
	Key        string   `toml:"key"`
	Dispatcher string   `toml:"dispatcher"`
	Argument   string   `toml:"argument"`
	Flags      string   `toml:"flags,omitempty"`
	Enabled    bool     `toml:"enabled"`
}

// WindowRuleConfig mirrors ipc.WindowRule with TOML tags.
type WindowRuleConfig struct {
	Match  string `toml:"match"`
	Rule   string `toml:"rule"`
	Action string `toml:"action"`
}

// ToIPCConfig converts the TOML configuration to the IPC ConfigUniversal type.
func (c *TOMLConfig) ToIPCConfig() ipc.ConfigUniversal {
	var cfg ipc.ConfigUniversal

	if c.Appearance != nil {
		cfg.Appearance = c.Appearance.toIPC()
	}

	if len(c.Keybinds) > 0 {
		var binds []ipc.Keybind
		for _, kb := range c.Keybinds {
			binds = append(binds, kb.toIPC())
		}
		cfg.Keybinds = ipc.ConfigKeybinds{
			Custom: binds,
		}
	}

	for _, wr := range c.WindowRules {
		cfg.WindowRules = append(cfg.WindowRules, ipc.WindowRule{
			Match:  wr.Match,
			Rule:   wr.Rule,
			Action: wr.Action,
		})
	}

	return cfg
}

// ToBatchKeybindsPayload converts keybinds to the IPC batch payload format.
func (c *TOMLConfig) ToBatchKeybindsPayload() ipc.BatchKeybindsPayload {
	var payload ipc.BatchKeybindsPayload
	for _, kb := range c.Keybinds {
		payload.Binds = append(payload.Binds, kb.toIPC())
	}
	return payload
}

func (a *AppearanceConfig) toIPC() ipc.ConfigAppearance {
	var cfg ipc.ConfigAppearance

	if a.Gaps != nil {
		cfg.Gaps = &ipc.Gaps{
			Inner: a.Gaps.Inner,
			Outer: a.Gaps.Outer,
		}
	}
	if a.Border != nil {
		cfg.Border = &ipc.Border{
			Width:         a.Border.Width,
			ActiveColor:   a.Border.ActiveColor,
			InactiveColor: a.Border.InactiveColor,
			Rounding:      a.Border.Rounding,
		}
	}
	if a.Opacity != nil {
		cfg.Opacity = &ipc.Opacity{
			Active:   a.Opacity.Active,
			Inactive: a.Opacity.Inactive,
		}
	}
	if a.Blur != nil {
		cfg.Blur = &ipc.Blur{
			Enabled: a.Blur.Enabled,
			Size:    a.Blur.Size,
			Passes:  a.Blur.Passes,
		}
	}
	if a.Shadow != nil {
		cfg.Shadow = &ipc.Shadow{
			Enabled: a.Shadow.Enabled,
			Size:    a.Shadow.Size,
			Color:   a.Shadow.Color,
		}
	}
	if a.Animations != nil {
		cfg.Animations = &ipc.Animations{
			Enabled: a.Animations.Enabled,
		}
	}

	return cfg
}

func (kb *KeybindConfig) toIPC() ipc.Keybind {
	return ipc.Keybind{
		Modifiers:  kb.Modifiers,
		Key:        kb.Key,
		Dispatcher: kb.Dispatcher,
		Argument:   kb.Argument,
		Flags:      kb.Flags,
		Enabled:    kb.Enabled,
	}
}
