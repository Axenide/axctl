package niri

import (
	"strings"
	"testing"

	"axctl/pkg/ipc"
)

func intPtr(v int) *int       { return &v }
func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

func TestGenerateAppearanceGapsAndBorder(t *testing.T) {
	g := &Generator{}
	out := g.GenerateAppearance(ipc.ConfigAppearance{
		Gaps:   &ipc.Gaps{Inner: intPtr(8)},
		Border: &ipc.Border{Width: intPtr(3), ActiveColor: strPtr("#ff8800")},
	})
	if !strings.Contains(out, "layout {") {
		t.Fatalf("expected layout block, got: %s", out)
	}
	if !strings.Contains(out, "gaps 8") {
		t.Fatalf("expected gaps 8, got: %s", out)
	}
	if !strings.Contains(out, "border {") {
		t.Fatalf("expected border block, got: %s", out)
	}
	if !strings.Contains(out, "width 3") {
		t.Fatalf("expected width 3, got: %s", out)
	}
	if !strings.Contains(out, "#ff8800") {
		t.Fatalf("expected active-color with #ff8800, got: %s", out)
	}
}

func TestGenerateAppearanceUnsupportedWarns(t *testing.T) {
	g := &Generator{}
	out := g.GenerateAppearance(ipc.ConfigAppearance{
		Gaps:    &ipc.Gaps{Outer: intPtr(5)},
		Opacity: &ipc.Opacity{Active: floatPtr(0.9)},
		Blur:    &ipc.Blur{Enabled: boolPtr(true)},
	})
	if !strings.Contains(out, "outer gaps") {
		t.Fatalf("expected outer gaps warning, got: %s", out)
	}
	if !strings.Contains(out, "opacity") {
		t.Fatalf("expected opacity warning, got: %s", out)
	}
	if !strings.Contains(out, "blur") {
		t.Fatalf("expected blur warning, got: %s", out)
	}
}

func TestGenerateAppearanceAnimationsOff(t *testing.T) {
	g := &Generator{}
	out := g.GenerateAppearance(ipc.ConfigAppearance{
		Animations: &ipc.Animations{Enabled: boolPtr(false)},
	})
	if !strings.Contains(out, "animations {") || !strings.Contains(out, "off") {
		t.Fatalf("expected animations block with off, got: %s", out)
	}
}

// TestGenerateAppearanceAnimationsOn omits the animations block when
// enabled — Niri has no `enabled` key inside `animations {}`, and the
// default state is already enabled, so emitting nothing keeps the file
// parseable.
func TestGenerateAppearanceAnimationsOn(t *testing.T) {
	g := &Generator{}
	out := g.GenerateAppearance(ipc.ConfigAppearance{
		Animations: &ipc.Animations{Enabled: boolPtr(true)},
	})
	if strings.Contains(out, "animations {") {
		t.Fatalf("animations block must be omitted when enabled, got: %s", out)
	}
}

func TestGenerateKeybindsSuperMapsToMod(t *testing.T) {
	g := &Generator{}
	out := g.GenerateKeybinds(ipc.ConfigKeybinds{
		Custom: []ipc.Keybind{{
			Modifiers:  []string{"SUPER"},
			Key:        "Return",
			Dispatcher: "exec",
			Argument:   "kitty",
			Enabled:    true,
		}},
	})
	if !strings.Contains(out, "binds {") {
		t.Fatalf("expected binds block, got: %s", out)
	}
	if !strings.Contains(out, "Mod+Return") {
		t.Fatalf("expected Mod+Return, got: %s", out)
	}
	if !strings.Contains(out, `spawn "kitty"`) {
		t.Fatalf("expected spawn kitty, got: %s", out)
	}
}

func TestGenerateKeybindsWithArguments(t *testing.T) {
	g := &Generator{}
	out := g.GenerateKeybinds(ipc.ConfigKeybinds{
		Custom: []ipc.Keybind{{
			Modifiers:  []string{"SUPER", "SHIFT"},
			Key:        "E",
			Dispatcher: "exec",
			Argument:   `thunar /tmp`,
			Enabled:    true,
		}},
	})
	if !strings.Contains(out, "Mod+Shift+E") {
		t.Fatalf("expected Mod+Shift+E, got: %s", out)
	}
	if !strings.Contains(out, `spawn "thunar" "/tmp"`) {
		t.Fatalf("expected tokenized spawn, got: %s", out)
	}
}

func TestGenerateKeybindsSkipsDisabled(t *testing.T) {
	g := &Generator{}
	out := g.GenerateKeybinds(ipc.ConfigKeybinds{
		Custom: []ipc.Keybind{{Key: "X", Dispatcher: "exec", Argument: "true", Enabled: false}},
	})
	if strings.Contains(out, "X {") {
		t.Fatalf("expected disabled bind to be omitted, got: %s", out)
	}
}

func TestGenerateWindowRulesLegacy(t *testing.T) {
	g := &Generator{}
	out := g.GenerateWindowRules([]ipc.WindowRule{
		{Match: "class:firefox", Rule: "opacity 0.95"},
	})
	if !strings.Contains(out, "window-rule {") {
		t.Fatalf("expected window-rule block, got: %s", out)
	}
	if !strings.Contains(out, "match app-id=") {
		t.Fatalf("expected match app-id, got: %s", out)
	}
	if !strings.Contains(out, "opacity 0.95") {
		t.Fatalf("expected opacity rule, got: %s", out)
	}
}

func TestGenerateWindowRulesBlockSyntax(t *testing.T) {
	g := &Generator{}
	out := g.GenerateWindowRules([]ipc.WindowRule{
		{Match: "class:kitty", Float: boolPtr(true), Rounding: intPtr(12)},
	})
	if !strings.Contains(out, "open-floating true") {
		t.Fatalf("expected open-floating, got: %s", out)
	}
	if !strings.Contains(out, "geometry-corner-radius 12") {
		t.Fatalf("expected geometry-corner-radius, got: %s", out)
	}
}

func TestGenerateLayerRulesIsNoOp(t *testing.T) {
	g := &Generator{}
	out := g.GenerateLayerRules([]ipc.LayerRule{{Namespace: "foo"}})
	if !strings.Contains(out, "not supported") {
		t.Fatalf("expected unsupported comment, got: %s", out)
	}
}

func TestGenerateStartupSpawnAtStartup(t *testing.T) {
	g := &Generator{}
	out := g.GenerateStartup([]string{"reload-some-service"}, []string{"waybar", "wl-paste --watch cliphist store"})
	if !strings.Contains(out, `spawn-at-startup "waybar"`) {
		t.Fatalf("expected spawn-at-startup waybar, got: %s", out)
	}
	if !strings.Contains(out, `spawn-at-startup "wl-paste --watch cliphist store"`) {
		t.Fatalf("expected spawn-at-startup with args, got: %s", out)
	}
	if !strings.Contains(out, "does not re-run exec") {
		t.Fatalf("expected exec comment, got: %s", out)
	}
}

func floatPtr(v float64) *float64 { return &v }

// TestNiriMapKeyMouseButtons checks the Hyprland mouse:N → Niri MouseXxx
// translation. Without this the emitted KDL is "mouse:272" which is an
// invalid identifier and breaks `niri validate`.
func TestNiriMapKeyMouseButtons(t *testing.T) {
	cases := map[string]string{
		"mouse:272": "MouseLeft",
		"mouse:273": "MouseRight",
		"mouse:274": "MouseMiddle",
		"mouse:275": "MouseForward",
		"mouse:276": "MouseBack",
	}
	for in, want := range cases {
		got, ok := niriMapKey(in)
		if !ok || got != want {
			t.Errorf("niriMapKey(%q) = (%q, %v), want (%q, true)", in, got, ok, want)
		}
	}
}

// TestNiriMapKeyLidSwitchSkipped ensures lid switch events aren't emitted
// at all — Niri has no equivalent, and "switch:Lid Switch" is invalid KDL.
func TestNiriMapKeyLidSwitchSkipped(t *testing.T) {
	for _, in := range []string{"switch:Lid Switch", "switch:on:Lid Switch", "switch:off:Lid Switch"} {
		if _, ok := niriMapKey(in); ok {
			t.Errorf("niriMapKey(%q) should be skipped, but was kept", in)
		}
	}
}

// TestNiriMapKeyXF86Passthrough covers keys that Niri accepts verbatim.
func TestNiriMapKeyXF86Passthrough(t *testing.T) {
	for _, in := range []string{"XF86AudioLowerVolume", "XF86AudioRaiseVolume", "F1", "Escape"} {
		got, ok := niriMapKey(in)
		if !ok || got != in {
			t.Errorf("niriMapKey(%q) = (%q, %v), want (%q, true)", in, got, ok, in)
		}
	}
}

// TestNiriParseColorRGBWrapper covers the Hyprland rgb()/rgba() format
// that ambxst writes into the TOML. Niri rejects "rgb(87abf8)" as invalid
// hex, so the wrapper must be unwrapped here.
func TestNiriParseColorRGBWrapper(t *testing.T) {
	cases := map[string]string{
		"rgb(87abf8)":    "#87abf8ff",
		"rgba(87abf880)": "#87abf880",
		"rgb(272937)":    "#272937ff",
		"#abcdef":        "#abcdefff",
		"#abcdef80":      "#abcdef80",
		"":               "",
	}
	for in, want := range cases {
		if got := niriParseColor(in); got != want {
			t.Errorf("niriParseColor(%q) = %q, want %q", in, got, want)
		}
	}
}

// a lid switch keybind into GenerateKeybinds must not produce any "switch:"
// a lid switch keybind into GenerateKeybinds must not produce a bind block
// for it. The skipped bind is reported in a comment so the user can wire
// it up by hand if they want.
func TestKeybindsSkipsUnsupportedLidSwitch(t *testing.T) {
	g := &Generator{}
	out := g.GenerateKeybinds(ipc.ConfigKeybinds{
		Custom: []ipc.Keybind{{
			Key:        "switch:Lid Switch",
			Enabled:    true,
			Dispatcher: "exec",
			Argument:   "true",
		}},
	})
	// No "    switch:" indent in the binds block — only the skipped-comment
	// line may mention it.
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "    switch:") {
			t.Errorf("lid switch leaked into binds block: %q", line)
		}
	}
	if !strings.Contains(out, "// Skipped binds") {
		t.Errorf("expected skipped binds comment, got: %s", out)
	}
}

func TestGenerateKeybindsCompositorDispatchers(t *testing.T) {
	g := &Generator{}
	bind := func(dispatcher, argument string) string {
		out := g.GenerateKeybinds(ipc.ConfigKeybinds{
			Custom: []ipc.Keybind{{Modifiers: []string{"SUPER"}, Key: "Q", Dispatcher: dispatcher, Argument: argument, Enabled: true}},
		})
		return out
	}
	cases := map[string]string{
		"killactive":              "close-window",
		"exit":                    "quit",
		"workspace 3":             "focus-workspace 3",
		"workspace e+1":           "focus-workspace-down",
		"workspace e-1":           "focus-workspace-up",
		"workspace chat":          `focus-workspace "chat"`,
		"movefocus l":             "focus-column-left",
		"movefocus r":             "focus-column-right",
		"movefocus u":             "focus-window-up",
		"movefocus d":             "focus-window-down",
		"movewindow l":            "move-column-left",
		"movewindow d":            "move-window-down",
		"movetoworkspace 5":       "move-window-to-workspace 5",
		"movetoworkspace e+1":     "move-window-to-workspace-down",
		"movetoworkspacesilent 7": "move-window-to-workspace 7 focus=false",
		"resizeactive -20 0":      `set-column-width "-20"`,
		"resizeactive 0 -15":      `set-window-height "-15"`,
	}
	for input, want := range cases {
		parts := strings.Fields(input)
		dispatcher := parts[0]
		argument := strings.TrimPrefix(input, dispatcher)
		argument = strings.TrimSpace(argument)
		out := bind(dispatcher, argument)
		if !strings.Contains(out, want) {
			t.Fatalf("%q: expected %q in output:\n%s", input, want, out)
		}
	}
}

func TestGenerateKeybindsSkipsHyprlandOnly(t *testing.T) {
	g := &Generator{}
	out := g.GenerateKeybinds(ipc.ConfigKeybinds{
		Custom: []ipc.Keybind{
			{Modifiers: []string{"SUPER"}, Key: "Q", Dispatcher: "togglespecialworkspace", Enabled: true},
			{Modifiers: []string{"SUPER"}, Key: "W", Dispatcher: "layoutmsg", Argument: "togglesplit", Enabled: true},
			{Modifiers: []string{"SUPER"}, Key: "E", Dispatcher: "movefocus", Argument: "x", Enabled: true},
		},
	})
	if strings.Contains(out, "SUPER+Q {") || strings.Contains(out, "SUPER+W {") || strings.Contains(out, "SUPER+E {") {
		t.Fatalf("hyprland-only binds must be skipped:\n%s", out)
	}
	if !strings.Contains(out, "Skipped binds") {
		t.Fatalf("expected skipped comment, got:\n%s", out)
	}
}

func TestGenerateAppearanceBorderOnAndFocusRingOff(t *testing.T) {
	g := &Generator{}
	out := g.GenerateAppearance(ipc.ConfigAppearance{
		Border: &ipc.Border{Width: intPtr(2), ActiveColor: strPtr("#ff0000")},
	})
	if !strings.Contains(out, "focus-ring {\n        off\n    }") {
		t.Fatalf("expected focus-ring off when border is managed, got:\n%s", out)
	}
	if !strings.Contains(out, "border {\n        on\n") {
		t.Fatalf("expected border on flag, got:\n%s", out)
	}
}

func TestGenerateAppearanceRoundingWindowRule(t *testing.T) {
	g := &Generator{}
	out := g.GenerateAppearance(ipc.ConfigAppearance{
		Border: &ipc.Border{Rounding: intPtr(12)},
	})
	if !strings.Contains(out, "window-rule {\n    geometry-corner-radius 12\n    clip-to-geometry true\n}") {
		t.Fatalf("expected global rounding window-rule, got:\n%s", out)
	}
	if strings.Contains(out, "border rounding") {
		t.Fatalf("rounding should no longer be listed as unsupported:\n%s", out)
	}
}

func TestGenerateKeybindsSkipsModifierSelfBind(t *testing.T) {
	g := &Generator{}
	out := g.GenerateKeybinds(ipc.ConfigKeybinds{
		Custom: []ipc.Keybind{
			{Modifiers: []string{"SUPER"}, Key: "Super_L", Dispatcher: "exec", Argument: "ambxst run launcher", Enabled: true},
			{Modifiers: []string{"ALT"}, Key: "Alt_L", Dispatcher: "exec", Argument: "true", Enabled: true},
			{Modifiers: []string{"SUPER"}, Key: "T", Dispatcher: "exec", Argument: "foot", Enabled: true},
		},
	})
	if strings.Contains(out, "Mod+Super_L") {
		t.Fatalf("modifier-self bind must be skipped (niri fires binds on press):\n%s", out)
	}
	if strings.Contains(out, "Alt+Alt_L") {
		t.Fatalf("Alt-self bind must be skipped:\n%s", out)
	}
	if !strings.Contains(out, "Mod+T") {
		t.Fatalf("normal binds must survive:\n%s", out)
	}
	if !strings.Contains(out, "fires binds on press") {
		t.Fatalf("expected explanatory skip comment:\n%s", out)
	}
}
