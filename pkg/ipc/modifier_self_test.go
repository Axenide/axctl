package ipc

import "testing"

func TestModifierSelfGroup(t *testing.T) {
	tests := []struct {
		name string
		kb   Keybind
		want string
	}{
		{
			name: "super_l alone",
			kb:   Keybind{Enabled: true, Modifiers: []string{"SUPER"}, Key: "Super_L", Dispatcher: "exec", Argument: "ambxst run launcher"},
			want: "SUPER",
		},
		{
			name: "super_r alone",
			kb:   Keybind{Enabled: true, Modifiers: []string{"SUPER"}, Key: "Super_R", Dispatcher: "spawn", Argument: "true"},
			want: "SUPER",
		},
		{
			name: "alt_l alone",
			kb:   Keybind{Enabled: true, Modifiers: []string{"ALT"}, Key: "Alt_L", Dispatcher: "exec", Argument: "true"},
			want: "ALT",
		},
		{
			name: "ctrl_l alone without dispatcher",
			kb:   Keybind{Enabled: true, Modifiers: []string{"CTRL"}, Key: "Control_L", Argument: "true"},
			want: "CTRL",
		},
		{
			name: "shift_r alone",
			kb:   Keybind{Enabled: true, Modifiers: []string{"shift"}, Key: "Shift_R", Dispatcher: "exec", Argument: "true"},
			want: "SHIFT",
		},
		{
			name: "super_l with wrong modifier",
			kb:   Keybind{Enabled: true, Modifiers: []string{"ALT"}, Key: "Super_L", Dispatcher: "exec", Argument: "true"},
			want: "",
		},
		{
			name: "super_l with modifier alias",
			kb:   Keybind{Enabled: true, Modifiers: []string{"MOD"}, Key: "Super_L", Dispatcher: "exec", Argument: "true"},
			want: "",
		},
		{
			name: "two modifiers",
			kb:   Keybind{Enabled: true, Modifiers: []string{"SUPER", "SHIFT"}, Key: "Super_L", Dispatcher: "exec", Argument: "true"},
			want: "",
		},
		{
			name: "no modifiers",
			kb:   Keybind{Enabled: true, Key: "Super_L", Dispatcher: "exec", Argument: "true"},
			want: "",
		},
		{
			name: "disabled bind",
			kb:   Keybind{Enabled: false, Modifiers: []string{"SUPER"}, Key: "Super_L", Dispatcher: "exec", Argument: "true"},
			want: "",
		},
		{
			name: "non-exec dispatcher",
			kb:   Keybind{Enabled: true, Modifiers: []string{"SUPER"}, Key: "Super_L", Dispatcher: "killactive"},
			want: "",
		},
		{
			name: "ordinary key",
			kb:   Keybind{Enabled: true, Modifiers: []string{"SUPER"}, Key: "Q", Dispatcher: "exec", Argument: "kitty"},
			want: "",
		},
		{
			name: "caps lock is not a tracked group",
			kb:   Keybind{Enabled: true, Modifiers: []string{"CAPS"}, Key: "Caps_Lock", Dispatcher: "exec", Argument: "true"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ModifierSelfGroup(tt.kb); got != tt.want {
				t.Fatalf("ModifierSelfGroup() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestModifierSelfGroupFromParts(t *testing.T) {
	if got := ModifierSelfGroupFromParts([]string{"SUPER"}, "Super_L"); got != "SUPER" {
		t.Fatalf("ModifierSelfGroupFromParts() = %q, want %q", got, "SUPER")
	}
	if got := ModifierSelfGroupFromParts([]string{"SUPER"}, "Q"); got != "" {
		t.Fatalf("ModifierSelfGroupFromParts() = %q, want empty", got)
	}
}
