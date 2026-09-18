package server

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"axctl/pkg/ipc"
)

var errNoConfigState = errors.New("no config applied yet; use Config.Apply first")

// ConfigState caches the last applied ConfigUniversal payload so partial
// updates (Config.Set, Config.Batch, Config.KeybindsBatch) can regenerate
// the single generated config file without asking the client to resend the
// full state. The daemon seeds it from the TOML at startup and on reload.
type ConfigState struct {
	mu      sync.Mutex
	last    ipc.ConfigUniversal
	applied bool
}

func NewConfigState() *ConfigState {
	return &ConfigState{}
}

// Seed replaces the cached payload after a full config apply.
func (s *ConfigState) Seed(payload ipc.ConfigUniversal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = payload
	s.applied = true
}

// Has reports whether any config has been applied yet.
func (s *ConfigState) Has() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applied
}

// Current returns a copy of the cached payload, if any.
func (s *ConfigState) Current() (ipc.ConfigUniversal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.applied {
		return ipc.ConfigUniversal{}, false
	}
	return cloneConfigUniversal(s.last), true
}

// SetKey mutates one appearance key in the cached payload and returns the
// updated payload for regeneration.
func (s *ConfigState) SetKey(key string, value interface{}) (ipc.ConfigUniversal, error) {
	return s.SetKeys(map[string]interface{}{key: value})
}

// SetKeys applies several appearance keys atomically: either every key is
// valid and the payload updates, or nothing changes and the offending keys
// are reported.
func (s *ConfigState) SetKeys(configs map[string]interface{}) (ipc.ConfigUniversal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.applied {
		return ipc.ConfigUniversal{}, errNoConfigState
	}
	next := cloneConfigUniversal(s.last)
	unknown := []string{}
	for k, v := range configs {
		if !ipc.SetAppearanceKey(&next.Appearance, k, v) {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		return ipc.ConfigUniversal{}, errors.New("unsupported config keys: " + strings.Join(unknown, ", "))
	}
	s.last = next
	return cloneConfigUniversal(s.last), nil
}

// GetKey reads one appearance key back from the cached payload.
func (s *ConfigState) GetKey(key string) (interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.applied {
		return nil, errNoConfigState
	}
	v, ok := ipc.GetAppearanceKey(&s.last.Appearance, key)
	if !ok {
		return nil, errors.New("unsupported config key: " + key)
	}
	return v, nil
}

// ApplyKeybinds merges a batch keybinds payload into the cached payload and
// returns the updated payload for regeneration. Unbinds remove matching
// binds from every section (Ambxst and custom); binds are upserted into the
// custom set by modifiers+key, mirroring Hyprland's incremental semantics.
func (s *ConfigState) ApplyKeybinds(payload ipc.BatchKeybindsPayload) (ipc.ConfigUniversal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.applied {
		return ipc.ConfigUniversal{}, errNoConfigState
	}
	next := cloneConfigUniversal(s.last)
	applyKeybindsPayload(&next.Keybinds, payload)
	s.last = next
	return cloneConfigUniversal(s.last), nil
}

// cloneConfigUniversal deep-copies a payload via JSON round-trip so
// mutations never alias the cached state. All fields are JSON-tagged, so
// the round-trip is lossless.
func cloneConfigUniversal(p ipc.ConfigUniversal) ipc.ConfigUniversal {
	b, err := json.Marshal(p)
	if err != nil {
		return p
	}
	var out ipc.ConfigUniversal
	if err := json.Unmarshal(b, &out); err != nil {
		return p
	}
	return out
}

func keybindComboMatches(kb ipc.Keybind, mods, key string) bool {
	return kb.Key == key && strings.Join(kb.Modifiers, " ") == mods
}

// modifierSelfGroup reports whether a keybind is a "modifier key alone"
// bind: the trigger key is a modifier key and its only modifier is that
// same modifier (e.g. key Super_L with modifiers [SUPER]). The returned
// string is the keymon modifier group, or "" when the bind is not of that
// shape.
func modifierSelfGroup(kb ipc.Keybind) string {
	if !kb.Enabled || kb.Dispatcher != "exec" && kb.Dispatcher != "spawn" && kb.Dispatcher != "" {
		return ""
	}
	if len(kb.Modifiers) != 1 {
		return ""
	}
	group := ""
	switch kb.Key {
	case "Super_L", "Super_R":
		group = "SUPER"
	case "Alt_L", "Alt_R":
		group = "ALT"
	case "Control_L", "Control_R":
		group = "CTRL"
	case "Shift_L", "Shift_R":
		group = "SHIFT"
	default:
		return ""
	}
	var groupMod string
	switch group {
	case "SUPER":
		groupMod = "super"
	case "ALT":
		groupMod = "alt"
	case "CTRL":
		groupMod = "ctrl"
	case "SHIFT":
		groupMod = "shift"
	}
	if !strings.EqualFold(kb.Modifiers[0], groupMod) {
		return ""
	}
	return group
}

// keymonBindsFromPayload collects modifier-alone commands from a config
// payload. These binds are skipped in generated niri configs (niri fires
// binds on press only) and are handled by the keymon evdev monitor instead.
func keymonBindsFromPayload(payload ipc.ConfigUniversal) map[string]string {
	binds := map[string]string{}
	add := func(kb ipc.Keybind) {
		if group := modifierSelfGroup(kb); group != "" {
			binds[group] = kb.Argument
		}
	}
	if payload.Keybinds.Ambxst != nil {
		for _, kb := range payload.Keybinds.Ambxst.System {
			add(kb)
		}
		for _, kb := range payload.Keybinds.Ambxst.Binds {
			add(kb)
		}
	}
	for _, kb := range payload.Keybinds.Custom {
		add(kb)
	}
	return binds
}

func applyKeybindsPayload(cfg *ipc.ConfigKeybinds, payload ipc.BatchKeybindsPayload) {
	for _, u := range payload.Unbinds {
		mods := strings.Join(u.Modifiers, " ")
		if cfg.Ambxst != nil {
			for name, kb := range cfg.Ambxst.System {
				if keybindComboMatches(kb, mods, u.Key) {
					delete(cfg.Ambxst.System, name)
				}
			}
			for name, kb := range cfg.Ambxst.Binds {
				if keybindComboMatches(kb, mods, u.Key) {
					delete(cfg.Ambxst.Binds, name)
				}
			}
		}
		kept := make([]ipc.Keybind, 0, len(cfg.Custom))
		for _, kb := range cfg.Custom {
			if !keybindComboMatches(kb, mods, u.Key) {
				kept = append(kept, kb)
			}
		}
		cfg.Custom = kept
	}

	for _, b := range payload.Binds {
		mods := strings.Join(b.Modifiers, " ")
		replaced := false
		for i := range cfg.Custom {
			if keybindComboMatches(cfg.Custom[i], mods, b.Key) {
				cfg.Custom[i] = b
				replaced = true
				break
			}
		}
		if !replaced {
			cfg.Custom = append(cfg.Custom, b)
		}
	}
}
