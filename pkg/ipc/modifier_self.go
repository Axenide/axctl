package ipc

import "strings"

// ModifierSelfGroup reports whether a keybind is a "modifier key alone"
// bind: the trigger key is a modifier key and its only modifier is that
// same modifier (e.g. key Super_L with modifiers [SUPER]). The returned
// string is the keymon modifier group, or "" when the bind is not of that
// shape.
//
// These binds are not written to generated compositor configs: firing them
// from the compositor would either break modifier+key combos (press-only
// compositors like niri) or double-fire alongside the keymon evdev monitor,
// which implements the "modifier released alone" semantics for every
// compositor instead.
func ModifierSelfGroup(kb Keybind) string {
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

// ModifierSelfGroupFromParts is ModifierSelfGroup for binds assembled from
// raw parts (e.g. runtime single-bind requests where the dispatcher is
// always an exec command).
func ModifierSelfGroupFromParts(modifiers []string, key string) string {
	return ModifierSelfGroup(Keybind{
		Enabled:    true,
		Modifiers:  modifiers,
		Key:        key,
		Dispatcher: "exec",
	})
}
