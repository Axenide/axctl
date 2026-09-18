// Package keymon implements a minimal "keyd"-style modifier-alone detector.
// It observes raw evdev events from /dev/input (read-only, no grab, no
// uinput) and fires a registered command when a modifier key is pressed and
// released without any other key press in between. This reproduces the
// "press Super alone" behavior that compositors like niri cannot express
// with their press-only keybind system.
package keymon

import "fmt"

// Linux evdev constants (linux/input-event-codes.h).
const (
	evKey = 0x01
	evRel = 0x02

	keyLeftMeta  = 125
	keyRightMeta = 126

	relWheel    = 8
	relHWheel   = 9
	relWheelHi  = 10
	relHWheelHi = 11
)

// Modifier groups tracked by the machine, keyed by their evdev keycodes.
var modifierGroups = map[uint16]string{
	keyLeftMeta:   "SUPER",
	keyRightMeta:  "SUPER",
	keyLeftAlt:    "ALT",
	keyRightAlt:   "ALT",
	keyLeftCtrl:   "CTRL",
	keyRightCtrl:  "CTRL",
	keyLeftShift:  "SHIFT",
	keyRightShift: "SHIFT",
}

const (
	keyLeftAlt    = 56
	keyRightAlt   = 100
	keyLeftCtrl   = 29
	keyRightCtrl  = 97
	keyLeftShift  = 42
	keyRightShift = 54
)

// Machine is the pure event-driven state machine. Feed it evdev events and
// it reports when a modifier was pressed and released alone.
//
// Semantics mirror keyd's overload behavior:
//   - A press of a tracked modifier with no other key currently held starts
//     a candidate.
//   - Any other key or mouse button press (and any wheel scroll) while the
//     candidate is active cancels it.
//   - Releasing the last held tracked modifier while the candidate is still
//     active fires the modifier's group.
type Machine struct {
	held      map[uint16]bool
	modifiers map[uint16]bool
	candidate bool
}

func NewMachine() *Machine {
	return &Machine{
		held:      make(map[uint16]bool),
		modifiers: make(map[uint16]bool),
	}
}

// Process consumes one evdev event. When a modifier was released alone it
// returns the fired group (e.g. "SUPER").
func (m *Machine) Process(eventType uint16, code uint16, value int32) string {
	switch eventType {
	case evKey:
		return m.processKey(code, value)
	case evRel:
		// Wheel scrolling is real combo input (niri binds
		// Mod+WheelScrollDown); pointer movement is not.
		if code == relWheel || code == relHWheel || code == relWheelHi || code == relHWheelHi {
			m.candidate = false
		}
	}
	return ""
}

func (m *Machine) processKey(code uint16, value int32) string {
	switch value {
	case 1:
		if _, isMod := modifierGroups[code]; isMod {
			m.modifiers[code] = true
			if !m.candidate && len(m.held) == 0 {
				m.candidate = true
			}
			return ""
		}
		m.held[code] = true
		m.candidate = false
	case 0:
		if _, isMod := modifierGroups[code]; isMod {
			delete(m.modifiers, code)
			if m.candidate && len(m.modifiers) == 0 {
				m.candidate = false
				return modifierGroups[code]
			}
		} else {
			delete(m.held, code)
		}
	}
	return ""
}

// Fired is the event reported by the Monitor.
type Fired struct {
	Group   string // "SUPER", "ALT", "CTRL", "SHIFT"
	Command string
}

// String implements fmt.Stringer for logging.
func (f Fired) String() string {
	return fmt.Sprintf("%s alone -> %s", f.Group, f.Command)
}
