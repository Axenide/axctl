package ipc

import (
	"encoding/json"
)

// Gaps config
type Gaps struct {
	Inner *int `json:"inner,omitempty"`
	Outer *int `json:"outer,omitempty"`
}

// Border config
type Border struct {
	Width         *int    `json:"width,omitempty"`
	ActiveColor   *string `json:"active_color,omitempty"`
	InactiveColor *string `json:"inactive_color,omitempty"`
	Rounding      *int    `json:"rounding,omitempty"`
}

// Opacity config
type Opacity struct {
	Active   *float64 `json:"active,omitempty"`
	Inactive *float64 `json:"inactive,omitempty"`
}

// Blur config
type Blur struct {
	Enabled *bool `json:"enabled,omitempty"`
	Size    *int  `json:"size,omitempty"`
	Passes  *int  `json:"passes,omitempty"`
}

// Shadow config
type Shadow struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Size    *int    `json:"size,omitempty"`
	Color   *string `json:"color,omitempty"`
}

// Animations config
type Animations struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// ConfigAppearance holds universal configuration for UI and layout
type ConfigAppearance struct {
	Gaps       *Gaps       `json:"gaps,omitempty"`
	Border     *Border     `json:"border,omitempty"`
	Opacity    *Opacity    `json:"opacity,omitempty"`
	Blur       *Blur       `json:"blur,omitempty"`
	Shadow     *Shadow     `json:"shadow,omitempty"`
	Animations *Animations `json:"animations,omitempty"`
	Layout     *string     `json:"layout,omitempty"`
}

// Keybind represents a single keyboard shortcut
type Keybind struct {
	Modifiers  []string `json:"modifiers"`
	Key        string   `json:"key"`
	Dispatcher string   `json:"dispatcher"`
	Argument   string   `json:"argument"`
	Flags      string   `json:"flags,omitempty"`
	Enabled    bool     `json:"enabled"`
}

// KeybindTarget identifies a keybind by modifiers and key (for unbinding)
type KeybindTarget struct {
	Modifiers []string `json:"modifiers"`
	Key       string   `json:"key"`
}

// BatchKeybindsPayload is the structured payload for batch keybind operations.
// Clients send this as JSON; axctl translates to compositor-native syntax.
type BatchKeybindsPayload struct {
	Binds   []Keybind       `json:"binds"`
	Unbinds []KeybindTarget `json:"unbinds"`
}

// SystemKeybinds represents pre-defined system keybinds
type SystemKeybinds map[string]Keybind

// AmbxstKeybinds groups system and generic keybinds
type AmbxstKeybinds struct {
	System map[string]Keybind `json:"system,omitempty"`
	Binds  map[string]Keybind `json:"-"` // We will handle dynamic unmarshalling for non-system keys
}

// Custom unmarshaler for AmbxstKeybinds to handle dynamic keys vs "system"
func (a *AmbxstKeybinds) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	a.Binds = make(map[string]Keybind)

	for k, v := range raw {
		if k == "system" {
			if err := json.Unmarshal(v, &a.System); err != nil {
				return err
			}
		} else {
			var kb Keybind
			if err := json.Unmarshal(v, &kb); err != nil {
				return err
			}
			a.Binds[k] = kb
		}
	}
	return nil
}

// ConfigKeybinds holds the keybind structure
type ConfigKeybinds struct {
	Ambxst *AmbxstKeybinds `json:"ambxst,omitempty"`
	Custom []Keybind       `json:"custom,omitempty"`
}

// WindowRule represents a generic window rule
// Supports both legacy single-line syntax (match, rule, action) and
// block syntax with individual window rule properties.
type WindowRule struct {
	// Legacy single-line syntax fields (kept for backward compatibility)
	Match  string `json:"match"`
	Rule   string `json:"rule"`
	Action string `json:"action"`

	// Block syntax fields for granular window rule control
	// Float makes the window floating
	Float *bool `json:"float,omitempty"`
	// NoBlur disables blur effect on the window
	NoBlur *bool `json:"no_blur,omitempty"`
	// NoShadow disables shadow on the window
	NoShadow *bool `json:"no_shadow,omitempty"`
	// Rounding sets the window corner rounding (0 to disable)
	Rounding *int `json:"rounding,omitempty"`
	// BorderSize sets the window border size
	BorderSize *int `json:"border_size,omitempty"`
	// Pin pins the window to all workspaces
	Pin *bool `json:"pin,omitempty"`
	// Fullscreen sets the window to fullscreen state
	Fullscreen *bool `json:"fullscreen,omitempty"`
	// IdleInhibit inhibits idle timeout while window is focused
	IdleInhibit *bool `json:"idle_inhibit,omitempty"`
	// NoScreenShare disables screen sharing for the window
	NoScreenShare *bool `json:"no_screen_share,omitempty"`
	// Move sets the window position (e.g., "100,100" or "center")
	Move *string `json:"move,omitempty"`
	// Size sets the window size (e.g., "800x600" or "auto")
	Size *string `json:"size,omitempty"`
	// Name is the identifier for named windowrules (block syntax)
	Name string `json:"name,omitempty"`
}

// LayerRule represents a Hyprland layer rule configuration.
type LayerRule struct {
	NoAnim           *bool    `json:"no_anim,omitempty"`
	Blur             *bool    `json:"blur,omitempty"`
	BlurPopups       *bool    `json:"blur_popups,omitempty"`
	IgnoreAlpha      *bool    `json:"ignore_alpha,omitempty"`
	NoShadow         *bool    `json:"no_shadow,omitempty"`
	IgnoreZeroAlpha  *bool    `json:"ignore_zero_alpha,omitempty"`
	IgnoreAlphaValue *float64 `json:"ignore_alpha_value,omitempty"`
	Namespace        string   `json:"namespace"`
}

// ConfigUniversal holds the entire configuration state
type ConfigUniversal struct {
	Appearance  ConfigAppearance `json:"appearance"`
	Keybinds    ConfigKeybinds   `json:"keybinds"`
	WindowRules []WindowRule     `json:"window_rules"`
	LayerRules  []LayerRule      `json:"layer_rules"`
}

// ConfigGenerator transforms a universal configuration into a compositor-specific syntax
type ConfigGenerator interface {
	// GenerateAppearance outputs the configuration string for layout, colors, and decorations
	GenerateAppearance(config ConfigAppearance) string

	// GenerateKeybinds outputs the keybind declarations
	GenerateKeybinds(config ConfigKeybinds) string

	// GenerateWindowRules outputs the window rule declarations
	GenerateWindowRules(rules []WindowRule) string
	// GenerateLayerRules outputs the layer rule declarations
	GenerateLayerRules(rules []LayerRule) string
}
