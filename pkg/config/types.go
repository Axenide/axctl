package config

import "axctl/pkg/ipc"

// TOMLConfig is the top-level configuration structure parsed from TOML files.
type TOMLConfig struct {
	Include     []string           `toml:"include,omitempty"`
	Appearance  *AppearanceConfig  `toml:"appearance,omitempty"`
	General     *GeneralConfig     `toml:"general,omitempty"`
	Input       *InputConfig       `toml:"input,omitempty"`
	Keybinds    []KeybindConfig    `toml:"keybinds,omitempty"`
	WindowRules []WindowRuleConfig `toml:"window_rules,omitempty"`
	LayerRules  []LayerRuleConfig  `toml:"layer_rules,omitempty"`
	Startup     *StartupConfig     `toml:"startup,omitempty"`
	Exec        interface{}        `toml:"exec,omitempty"`
	ExecOnce    interface{}        `toml:"exec-once,omitempty"`
}

type StartupConfig struct {
	Exec     interface{} `toml:"exec,omitempty"`
	ExecOnce interface{} `toml:"exec-once,omitempty"`
}

type GeneralConfig struct {
	Layout string `toml:"layout,omitempty"`
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
// Supports both legacy single-line syntax (match, rule, action) and
// block syntax with individual window rule properties.
type WindowRuleConfig struct {
	// Legacy single-line syntax fields (kept for backward compatibility)
	Match  string `toml:"match"`
	Rule   string `toml:"rule"`
	Action string `toml:"action"`

	// Block syntax fields for granular window rule control
	// Float makes the window floating
	Float *bool `toml:"float,omitempty"`
	// NoBlur disables blur effect on the window
	NoBlur *bool `toml:"no_blur,omitempty"`
	// NoShadow disables shadow on the window
	NoShadow *bool `toml:"no_shadow,omitempty"`
	// Rounding sets the window corner rounding (0 to disable)
	Rounding *int `toml:"rounding,omitempty"`
	// BorderSize sets the window border size
	BorderSize *int `toml:"border_size,omitempty"`
	// Pin pins the window to all workspaces
	Pin *bool `toml:"pin,omitempty"`
	// Fullscreen sets the window to fullscreen state
	Fullscreen *bool `toml:"fullscreen,omitempty"`
	// IdleInhibit inhibits idle timeout while window is focused
	IdleInhibit *bool `toml:"idle_inhibit,omitempty"`
	// NoScreenShare disables screen sharing for the window
	NoScreenShare *bool `toml:"no_screen_share,omitempty"`
	// Move sets the window position (e.g., "100,100" or "center")
	Move *string `toml:"move,omitempty"`
	// Size sets the window size (e.g., "800x600" or "auto")
	Size *string `toml:"size,omitempty"`
}

// LayerRuleConfig mirrors Hyprland's layer rule settings.
// These rules control how layer surfaces (like wallpapers, lockscreens, notifications) are rendered.
type LayerRuleConfig struct {
	// NoAnim disables animations for the layer.
	NoAnim *bool `toml:"no_anim,omitempty"`
	// Blur enables blur effect on the layer.
	Blur *bool `toml:"blur,omitempty"`
	// BlurPopups enables blur on popups within this layer.
	BlurPopups *bool `toml:"blur_popups,omitempty"`
	// IgnoreAlpha ignores alpha channel when determining window opacity.
	IgnoreAlpha *bool `toml:"ignore_alpha,omitempty"`
	// NoShadow disables shadow rendering on the layer.
	NoShadow *bool `toml:"no_shadow,omitempty"`
	// IgnoreZeroAlpha ignores zero alpha values when calculating opacity.
	IgnoreZeroAlpha *bool `toml:"ignore_zero_alpha,omitempty"`
	// IgnoreAlphaValue sets a threshold for alpha value to be treated as opaque.
	IgnoreAlphaValue *float64 `toml:"ignore_alpha_value,omitempty"`
	// Namespace matches against the layer's namespace (e.g., "notifications", "waybar").
	Namespace string `toml:"namespace"`
}

// ToIPCConfig converts the TOML configuration to the IPC ConfigUniversal type.
func (c *TOMLConfig) ToIPCConfig() ipc.ConfigUniversal {
	var cfg ipc.ConfigUniversal

	if c.Appearance != nil {
		cfg.Appearance = c.Appearance.toIPC()
	}

	if c.General != nil && c.General.Layout != "" {
		cfg.Appearance.Layout = &c.General.Layout
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

			// Block syntax fields
			Float:         wr.Float,
			NoBlur:        wr.NoBlur,
			NoShadow:      wr.NoShadow,
			Rounding:      wr.Rounding,
			BorderSize:    wr.BorderSize,
			Pin:           wr.Pin,
			Fullscreen:    wr.Fullscreen,
			IdleInhibit:   wr.IdleInhibit,
			NoScreenShare: wr.NoScreenShare,
			Move:          wr.Move,
			Size:          wr.Size,
		})
	}

	for _, lr := range c.LayerRules {
		cfg.LayerRules = append(cfg.LayerRules, ipc.LayerRule{
			NoAnim:           lr.NoAnim,
			Blur:             lr.Blur,
			BlurPopups:       lr.BlurPopups,
			IgnoreAlpha:      lr.IgnoreAlpha,
			NoShadow:         lr.NoShadow,
			IgnoreZeroAlpha:  lr.IgnoreZeroAlpha,
			IgnoreAlphaValue: lr.IgnoreAlphaValue,
			Namespace:        lr.Namespace,
		})
	}

	if c.Startup != nil {
		appendExecCommands(&cfg.Exec, c.Startup.Exec)
		appendExecCommands(&cfg.ExecOnce, c.Startup.ExecOnce)
	}

	appendExecCommands(&cfg.Exec, c.Exec)
	appendExecCommands(&cfg.ExecOnce, c.ExecOnce)

	return cfg
}

func appendExecCommands(dst *[]string, raw interface{}) {
	if raw == nil {
		return
	}

	switch value := raw.(type) {
	case string:
		if value != "" {
			*dst = append(*dst, value)
		}
	case []string:
		for _, item := range value {
			if item != "" {
				*dst = append(*dst, item)
			}
		}
	case []interface{}:
		for _, item := range value {
			str, ok := item.(string)
			if ok && str != "" {
				*dst = append(*dst, str)
			}
		}
	}
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
