package server

import (
	"testing"

	"axctl/pkg/ipc"
)

func seededConfigState() *ConfigState {
	s := NewConfigState()
	inner := 8
	custom := []ipc.Keybind{{Modifiers: []string{"SUPER"}, Key: "T", Dispatcher: "exec", Argument: "foot", Enabled: true}}
	s.Seed(ipc.ConfigUniversal{
		Appearance: ipc.ConfigAppearance{
			Gaps: &ipc.Gaps{Inner: &inner},
		},
		Keybinds: ipc.ConfigKeybinds{
			Ambxst: &ipc.AmbxstKeybinds{
				System: map[string]ipc.Keybind{
					"terminal": {Modifiers: []string{"SUPER"}, Key: "Return", Dispatcher: "exec", Argument: "foot", Enabled: true},
				},
			},
			Custom: custom,
		},
	})
	return s
}

func TestConfigStateNotApplied(t *testing.T) {
	s := NewConfigState()
	if s.Has() {
		t.Fatal("fresh state should not be applied")
	}
	if _, err := s.SetKey("gaps.inner", 4); err == nil {
		t.Fatal("SetKey should fail before any seed")
	}
	if _, err := s.GetKey("gaps.inner"); err == nil {
		t.Fatal("GetKey should fail before any seed")
	}
	if _, err := s.ApplyKeybinds(ipc.BatchKeybindsPayload{}); err == nil {
		t.Fatal("ApplyKeybinds should fail before any seed")
	}
}

func TestConfigStateSetAndGetKey(t *testing.T) {
	s := seededConfigState()
	payload, err := s.SetKey("gaps.inner", float64(12))
	if err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if payload.Appearance.Gaps.Inner == nil || *payload.Appearance.Gaps.Inner != 12 {
		t.Fatalf("payload gaps.inner = %v, want 12", payload.Appearance.Gaps.Inner)
	}
	v, err := s.GetKey("gaps.inner")
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if v != 12 {
		t.Fatalf("GetKey = %v, want 12", v)
	}
}

func TestConfigStateSetKeyCoercion(t *testing.T) {
	s := seededConfigState()
	cases := map[string]any{
		"gaps.inner":          "16",
		"border.width":        float64(2),
		"border.active_color": "rgb(87abf8)",
		"opacity.active":      float64(0.9),
		"blur.enabled":        true,
		"blur.size":           int64(10),
	}
	for key, value := range cases {
		if _, err := s.SetKey(key, value); err != nil {
			t.Fatalf("SetKey(%q, %v): %v", key, value, err)
		}
	}
	if v, _ := s.GetKey("border.active_color"); v != "rgb(87abf8)" {
		t.Fatalf("border.active_color = %v", v)
	}
	if v, _ := s.GetKey("blur.size"); v != 10 {
		t.Fatalf("blur.size = %v, want 10", v)
	}
}

func TestConfigStateSetKeysAtomic(t *testing.T) {
	s := seededConfigState()
	if _, err := s.SetKeys(map[string]any{
		"gaps.inner": float64(20),
		"made.up":    float64(1),
	}); err == nil {
		t.Fatal("SetKeys should fail on unknown keys")
	}
	if v, _ := s.GetKey("gaps.inner"); v != 8 {
		t.Fatalf("gaps.inner = %v, want 8 (atomic rollback)", v)
	}
}

func TestConfigStateUnknownKey(t *testing.T) {
	s := seededConfigState()
	if _, err := s.SetKey("nope", float64(1)); err == nil {
		t.Fatal("SetKey should fail on unknown key")
	}
	if _, err := s.GetKey("nope"); err == nil {
		t.Fatal("GetKey should fail on unknown key")
	}
}

func TestConfigStateApplyKeybinds(t *testing.T) {
	s := seededConfigState()
	payload, err := s.ApplyKeybinds(ipc.BatchKeybindsPayload{
		Binds: []ipc.Keybind{
			{Modifiers: []string{"SUPER"}, Key: "T", Dispatcher: "exec", Argument: "kitty", Enabled: true},
			{Modifiers: []string{"SUPER", "SHIFT"}, Key: "E", Dispatcher: "exec", Argument: "notify-send hi", Enabled: true},
		},
		Unbinds: []ipc.KeybindTarget{
			{Modifiers: []string{"SUPER"}, Key: "Return"},
		},
	})
	if err != nil {
		t.Fatalf("ApplyKeybinds: %v", err)
	}

	if _, ok := payload.Keybinds.Ambxst.System["terminal"]; ok {
		t.Fatal("unbind should remove matching ambxst system bind")
	}

	var superT, superShiftE bool
	for _, kb := range payload.Keybinds.Custom {
		if kb.Key == "T" && len(kb.Modifiers) == 1 && kb.Modifiers[0] == "SUPER" {
			if kb.Argument != "kitty" {
				t.Fatalf("upserted bind argument = %q, want kitty", kb.Argument)
			}
			superT = true
		}
		if kb.Key == "E" && len(kb.Modifiers) == 2 && kb.Modifiers[0] == "SUPER" && kb.Modifiers[1] == "SHIFT" {
			superShiftE = true
		}
	}
	if !superT || !superShiftE {
		t.Fatalf("custom binds wrong after upsert: superT=%v superShiftE=%v", superT, superShiftE)
	}

	if s.last.Keybinds.Ambxst.System["terminal"].Argument == "foot" && len(s.last.Keybinds.Custom) == len(payload.Keybinds.Custom) {
		t.Fatal("cached state should reflect the merge")
	}
}
