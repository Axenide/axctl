package config

import "testing"

type keyboardSetterStub struct {
	layouts  string
	variants string
	calls    int
}

func (s *keyboardSetterStub) SetKeyboardLayouts(layouts, variants string) error {
	s.layouts = layouts
	s.variants = variants
	s.calls++
	return nil
}

func TestApplyKeyboardConfig(t *testing.T) {
	setter := &keyboardSetterStub{}
	if err := applyKeyboardConfig(setter, &KeyboardConfig{Layouts: " us ", Variants: " altgr-intl "}); err != nil {
		t.Fatal(err)
	}
	if setter.calls != 1 || setter.layouts != "us" || setter.variants != "altgr-intl" {
		t.Fatalf("unexpected keyboard settings call: %+v", setter)
	}
}

func TestApplyKeyboardConfigSkipsEmptyLayout(t *testing.T) {
	setter := &keyboardSetterStub{}
	if err := applyKeyboardConfig(setter, &KeyboardConfig{Variants: "altgr-intl"}); err != nil {
		t.Fatal(err)
	}
	if setter.calls != 0 {
		t.Fatalf("expected no keyboard settings call, got %d", setter.calls)
	}
}
