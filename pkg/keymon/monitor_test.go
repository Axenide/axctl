package keymon

import (
	"testing"
)

func handle(m *Monitor, evType uint16, code uint16, value int32) string {
	return m.handleEvent(evType, code, value)
}

func handlePress(m *Monitor, code uint16) string   { return handle(m, evKey, code, 1) }
func handleRelease(m *Monitor, code uint16) string { return handle(m, evKey, code, 0) }

func TestMonitorCrossDeviceClickCancelsCandidate(t *testing.T) {
	m := NewMonitor()
	if got := handlePress(m, keyLeftMeta); got != "" {
		t.Fatalf("super press fired early: %q", got)
	}
	if got := handlePress(m, btnLeft); got != "" {
		t.Fatalf("click fired: %q", got)
	}
	if got := handleRelease(m, btnLeft); got != "" {
		t.Fatalf("click release fired: %q", got)
	}
	if got := handleRelease(m, keyLeftMeta); got != "" {
		t.Fatalf("super release after cross-device click fired: %q", got)
	}
}

func TestMonitorSuperAloneStillFires(t *testing.T) {
	m := NewMonitor()
	if got := handlePress(m, keyLeftMeta); got != "" {
		t.Fatalf("super press fired early: %q", got)
	}
	if got := handleRelease(m, keyLeftMeta); got != "SUPER" {
		t.Fatalf("super alone release = %q, want SUPER", got)
	}
}

func TestMonitorCrossDeviceKeyCancelsCandidate(t *testing.T) {
	m := NewMonitor()
	handlePress(m, keyLeftMeta)
	handlePress(m, keyTestA)
	handleRelease(m, keyTestA)
	if got := handleRelease(m, keyLeftMeta); got != "" {
		t.Fatalf("super release after cross-device key fired: %q", got)
	}
}

func TestMonitorResetClearsStuckKeys(t *testing.T) {
	m := NewMonitor()
	handlePress(m, keyTestA)
	m.resetMachine()
	if got := handlePress(m, keyLeftMeta); got != "" {
		t.Fatalf("super press fired early: %q", got)
	}
	if got := handleRelease(m, keyLeftMeta); got != "SUPER" {
		t.Fatalf("super alone after reset = %q, want SUPER (reset must clear held keys)", got)
	}
}
