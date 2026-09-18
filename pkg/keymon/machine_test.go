package keymon

import (
	"testing"
)

func press(m *Machine, code uint16) string   { return m.Process(evKey, code, 1) }
func release(m *Machine, code uint16) string { return m.Process(evKey, code, 0) }
func repeat(m *Machine, code uint16) string  { return m.Process(evKey, code, 2) }

const (
	keyTestT = 20
	keyTestA = 30
	btnLeft  = 0x110
)

func TestSuperAloneFires(t *testing.T) {
	m := NewMachine()
	if got := press(m, keyLeftMeta); got != "" {
		t.Fatalf("super press fired early: %q", got)
	}
	if got := release(m, keyLeftMeta); got != "SUPER" {
		t.Fatalf("super alone release = %q, want SUPER", got)
	}
}

func TestSuperWithComboDoesNotFire(t *testing.T) {
	m := NewMachine()
	press(m, keyLeftMeta)
	press(m, keyTestT)
	release(m, keyTestT)
	if got := release(m, keyLeftMeta); got != "" {
		t.Fatalf("combo release fired: %q", got)
	}
}

func TestSuperRepeatDoesNotFire(t *testing.T) {
	m := NewMachine()
	press(m, keyLeftMeta)
	repeat(m, keyLeftMeta)
	repeat(m, keyLeftMeta)
	if got := release(m, keyLeftMeta); got != "SUPER" {
		t.Fatalf("super alone with repeats = %q, want SUPER", got)
	}
}

func TestKeyHeldBeforeSuperBlocksCandidate(t *testing.T) {
	m := NewMachine()
	press(m, keyTestA)
	press(m, keyLeftMeta)
	release(m, keyA)
	if got := release(m, keyLeftMeta); got != "" {
		t.Fatalf("release after held key fired: %q", got)
	}
}

func TestMouseClickCancelsCandidate(t *testing.T) {
	m := NewMachine()
	press(m, keyLeftMeta)
	if got := press(m, btnLeft); got != "" {
		t.Fatalf("click fired: %q", got)
	}
	if got := release(m, btnLeft); got != "" {
		t.Fatalf("click release fired: %q", got)
	}
	if got := release(m, keyLeftMeta); got != "" {
		t.Fatalf("super release after click fired: %q", got)
	}
}

func TestWheelScrollCancelsCandidate(t *testing.T) {
	m := NewMachine()
	press(m, keyLeftMeta)
	if got := m.Process(evRel, relWheel, 1); got != "" {
		t.Fatalf("wheel fired: %q", got)
	}
	if got := release(m, keyLeftMeta); got != "" {
		t.Fatalf("super release after wheel fired: %q", got)
	}
}

func TestPointerMoveDoesNotCancelCandidate(t *testing.T) {
	m := NewMachine()
	press(m, keyLeftMeta)
	if got := m.Process(evRel, 0, -3); got != "" {
		t.Fatalf("pointer move fired: %q", got)
	}
	if got := release(m, keyLeftMeta); got != "SUPER" {
		t.Fatalf("super alone after pointer move = %q, want SUPER", got)
	}
}

func TestBothSupersFireOnLastRelease(t *testing.T) {
	m := NewMachine()
	press(m, keyLeftMeta)
	press(m, keyRightMeta)
	release(m, keyLeftMeta)
	if got := release(m, keyRightMeta); got != "SUPER" {
		t.Fatalf("last super release = %q, want SUPER", got)
	}
}

func TestSecondModifierAloneFiresSecondGroup(t *testing.T) {
	m := NewMachine()
	press(m, keyLeftAlt)
	release(m, keyLeftAlt)
	press(m, keyLeftMeta)
	if got := release(m, keyLeftMeta); got != "SUPER" {
		t.Fatalf("super after alt-alone = %q, want SUPER", got)
	}
}

func TestSecondSuperReleaseDoesNotDoubleFire(t *testing.T) {
	m := NewMachine()
	press(m, keyLeftMeta)
	release(m, keyLeftMeta)
	if got := release(m, keyLeftMeta); got != "" {
		t.Fatalf("release of non-held key fired: %q", got)
	}
}
