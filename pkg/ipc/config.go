package ipc

import (
	"encoding/json"
	"fmt"
	"strings"
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
	Enabled        *bool   `json:"enabled,omitempty"`
	// WorkspaceStyle overrides the default 'slidefade 20%' used for
	// the workspaces animation in the generated hyprland.{lua,conf}.
	// Shell code (e.g. ambxst) computes this from the bar orientation:
	// horizontal bar → vertical slide ('slidefadevert 20%'), vertical
	// bar → horizontal slide ('slidefade 20%'). When nil, axctl uses
	// 'slidefade 20%' to preserve prior behaviour.
	WorkspaceStyle *string `json:"workspaceStyle,omitempty"`
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
	// PlaceWithinBackdrop moves the layer surface into niri's overview
	// backdrop (niri-only). Niri-specific: other compositors ignore it.
	PlaceWithinBackdrop *bool  `json:"place_within_backdrop,omitempty"`
	Namespace           string `json:"namespace"`
}

// ConfigUniversal holds the entire configuration state
type ConfigUniversal struct {
	Appearance  ConfigAppearance `json:"appearance"`
	Keybinds    ConfigKeybinds   `json:"keybinds"`
	WindowRules []WindowRule     `json:"window_rules"`
	LayerRules  []LayerRule      `json:"layer_rules"`
	Exec        []string         `json:"exec,omitempty"`
	ExecOnce    []string         `json:"exec_once,omitempty"`
}

// ConfigGenerator transforms a universal configuration into compositor-specific hyprlang syntax
type ConfigGenerator interface {
	// GenerateAppearance outputs the configuration string for layout, colors, and decorations
	GenerateAppearance(config ConfigAppearance) string

	// GenerateKeybinds outputs the keybind declarations
	GenerateKeybinds(config ConfigKeybinds) string

	// GenerateWindowRules outputs the window rule declarations
	GenerateWindowRules(rules []WindowRule) string
	// GenerateLayerRules outputs the layer rule declarations
	GenerateLayerRules(rules []LayerRule) string
	GenerateStartup(exec []string, execOnce []string) string
}

// LuaConfigGenerator transforms a universal configuration into Hyprland Lua syntax
type LuaConfigGenerator interface {
	GenerateAppearanceLua(config ConfigAppearance) string
	GenerateKeybindsLua(config ConfigKeybinds) string
	GenerateWindowRulesLua(rules []WindowRule) string
	GenerateLayerRulesLua(rules []LayerRule) string
	GenerateStartupLua(exec []string, execOnce []string) string
}

// SetAppearanceKey sets one dot-notated appearance key (e.g. "gaps.inner")
// in the universal appearance config. The key set mirrors the Hyprland
// BatchConfig mapping so every compositor accepts the same names.
// Returns false for unknown keys or values of the wrong type.
func SetAppearanceKey(a *ConfigAppearance, key string, value interface{}) bool {
	switch key {
	case "gaps.inner":
		if v, ok := coerceInt(value); ok {
			a.ensureGaps().Inner = v
			return true
		}
	case "gaps.outer":
		if v, ok := coerceInt(value); ok {
			a.ensureGaps().Outer = v
			return true
		}
	case "border.width":
		if v, ok := coerceInt(value); ok {
			a.ensureBorder().Width = v
			return true
		}
	case "border.active_color":
		if v, ok := coerceString(value); ok {
			a.ensureBorder().ActiveColor = v
			return true
		}
	case "border.inactive_color":
		if v, ok := coerceString(value); ok {
			a.ensureBorder().InactiveColor = v
			return true
		}
	case "opacity.active":
		if v, ok := coerceFloat(value); ok {
			a.ensureOpacity().Active = v
			return true
		}
	case "opacity.inactive":
		if v, ok := coerceFloat(value); ok {
			a.ensureOpacity().Inactive = v
			return true
		}
	case "blur.enabled":
		if v, ok := coerceBool(value); ok {
			a.ensureBlur().Enabled = v
			return true
		}
	case "blur.size":
		if v, ok := coerceInt(value); ok {
			a.ensureBlur().Size = v
			return true
		}
	case "blur.passes":
		if v, ok := coerceInt(value); ok {
			a.ensureBlur().Passes = v
			return true
		}
	}
	return false
}

// GetAppearanceKey reads one dot-notated appearance key back from the
// universal appearance config. Unset values return (nil, true); unknown
// keys return (nil, false).
func GetAppearanceKey(a *ConfigAppearance, key string) (interface{}, bool) {
	switch key {
	case "gaps.inner":
		if a.Gaps == nil {
			return nil, true
		}
		return derefInt(a.Gaps.Inner), true
	case "gaps.outer":
		if a.Gaps == nil {
			return nil, true
		}
		return derefInt(a.Gaps.Outer), true
	case "border.width":
		if a.Border == nil {
			return nil, true
		}
		return derefInt(a.Border.Width), true
	case "border.active_color":
		if a.Border == nil {
			return nil, true
		}
		return derefString(a.Border.ActiveColor), true
	case "border.inactive_color":
		if a.Border == nil {
			return nil, true
		}
		return derefString(a.Border.InactiveColor), true
	case "opacity.active":
		if a.Opacity == nil {
			return nil, true
		}
		return derefFloat(a.Opacity.Active), true
	case "opacity.inactive":
		if a.Opacity == nil {
			return nil, true
		}
		return derefFloat(a.Opacity.Inactive), true
	case "blur.enabled":
		if a.Blur == nil {
			return nil, true
		}
		return derefBool(a.Blur.Enabled), true
	case "blur.size":
		if a.Blur == nil {
			return nil, true
		}
		return derefInt(a.Blur.Size), true
	case "blur.passes":
		if a.Blur == nil {
			return nil, true
		}
		return derefInt(a.Blur.Passes), true
	}
	return nil, false
}

func (a *ConfigAppearance) ensureGaps() *Gaps {
	if a.Gaps == nil {
		a.Gaps = &Gaps{}
	}
	return a.Gaps
}

func (a *ConfigAppearance) ensureBorder() *Border {
	if a.Border == nil {
		a.Border = &Border{}
	}
	return a.Border
}

func (a *ConfigAppearance) ensureOpacity() *Opacity {
	if a.Opacity == nil {
		a.Opacity = &Opacity{}
	}
	return a.Opacity
}

func (a *ConfigAppearance) ensureBlur() *Blur {
	if a.Blur == nil {
		a.Blur = &Blur{}
	}
	return a.Blur
}

// coerceInt accepts JSON-decoded numbers (float64), Go integers, and
// numeric strings, returning a pointer to the parsed int.
func coerceInt(value interface{}) (*int, bool) {
	switch t := value.(type) {
	case float64:
		v := int(t)
		return &v, true
	case int:
		return &t, true
	case int64:
		v := int(t)
		return &v, true
	case string:
		var v int
		if _, err := fmt.Sscanf(strings.TrimSpace(t), "%d", &v); err == nil {
			return &v, true
		}
	}
	return nil, false
}

func coerceFloat(value interface{}) (*float64, bool) {
	switch t := value.(type) {
	case float64:
		return &t, true
	case int:
		v := float64(t)
		return &v, true
	case int64:
		v := float64(t)
		return &v, true
	case string:
		var v float64
		if _, err := fmt.Sscanf(strings.TrimSpace(t), "%g", &v); err == nil {
			return &v, true
		}
	}
	return nil, false
}

func coerceBool(value interface{}) (*bool, bool) {
	switch t := value.(type) {
	case bool:
		return &t, true
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true":
			v := true
			return &v, true
		case "false":
			v := false
			return &v, true
		}
	}
	return nil, false
}

func coerceString(value interface{}) (*string, bool) {
	if t, ok := value.(string); ok {
		return &t, true
	}
	return nil, false
}

func derefInt(v *int) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func derefFloat(v *float64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func derefBool(v *bool) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func derefString(v *string) interface{} {
	if v == nil {
		return nil
	}
	return *v
}
