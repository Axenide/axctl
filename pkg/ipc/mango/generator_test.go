package mango

import (
	"strings"
	"testing"

	"axctl/pkg/ipc"
)

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }
func strPtr(s string) *string     { return &s }
func boolPtr(b bool) *bool        { return &b }

func TestGenerateAppearanceGapsAndBorder(t *testing.T) {
	g := &Generator{}
	out := g.GenerateAppearance(ipc.ConfigAppearance{
		Gaps:   &ipc.Gaps{Inner: intPtr(8), Outer: intPtr(4)},
		Border: &ipc.Border{Width: intPtr(3), ActiveColor: strPtr("#ff8800"), InactiveColor: strPtr("333333"), Rounding: intPtr(6)},
	})
	if !strings.Contains(out, "gap_inner_horizontal = 8") || !strings.Contains(out, "gap_inner_vertical = 8") {
		t.Fatalf("expected inner gaps, got: %s", out)
	}
	if !strings.Contains(out, "gap_outer_horizontal = 4") || !strings.Contains(out, "gap_outer_vertical = 4") {
		t.Fatalf("expected outer gaps, got: %s", out)
	}
	if !strings.Contains(out, "border_px = 3") {
		t.Fatalf("expected border_px, got: %s", out)
	}
	if !strings.Contains(out, "focus_color = 0xff8800ff") {
		t.Fatalf("expected focus_color 0xff8800ff, got: %s", out)
	}
	if !strings.Contains(out, "border_color = 0x333333ff") {
		t.Fatalf("expected bordercolor, got: %s", out)
	}
	if !strings.Contains(out, "border_radius = 6") {
		t.Fatalf("expected border_radius, got: %s", out)
	}
}

func TestGenerateAppearanceBlurAndShadow(t *testing.T) {
	g := &Generator{}
	out := g.GenerateAppearance(ipc.ConfigAppearance{
		Blur:   &ipc.Blur{Enabled: boolPtr(true), Size: intPtr(5), Passes: intPtr(2)},
		Shadow: &ipc.Shadow{Enabled: boolPtr(true)},
	})
	if !strings.Contains(out, "blur = 1") {
		t.Fatalf("expected blur enabled, got: %s", out)
	}
	if !strings.Contains(out, "blur_params_radius = 5") {
		t.Fatalf("expected blur radius, got: %s", out)
	}
	if !strings.Contains(out, "blur_params_num_passes = 2") {
		t.Fatalf("expected blur passes, got: %s", out)
	}
	if !strings.Contains(out, "shadows = 1") {
		t.Fatalf("expected shadows enabled, got: %s", out)
	}
}

func TestGenerateAppearanceMapsWorkspaceAnimationDirection(t *testing.T) {
	for _, test := range []struct {
		style string
		want  string
	}{
		{style: "slidefade 20%", want: "tag_animation_direction = 1"},
		{style: "slidefadevert 20%", want: "tag_animation_direction = 0"},
	} {
		out := (&Generator{}).GenerateAppearance(ipc.ConfigAppearance{
			Animations: &ipc.Animations{Enabled: boolPtr(true), WorkspaceStyle: strPtr(test.style)},
		})
		if !strings.Contains(out, test.want) {
			t.Errorf("workspace style %q should generate %q, got:\n%s", test.style, test.want, out)
		}
	}
}

func TestGenerateKeybindsMapsDispatchers(t *testing.T) {
	g := &Generator{}
	out := g.GenerateKeybinds(ipc.ConfigKeybinds{
		Custom: []ipc.Keybind{
			{Modifiers: []string{"SUPER"}, Key: "Q", Dispatcher: "exec", Argument: "kitty", Enabled: true},
			{Modifiers: []string{"SUPER"}, Key: "C", Dispatcher: "close", Enabled: true},
			{Modifiers: []string{"SUPER"}, Key: "F", Dispatcher: "fullscreen", Enabled: true},
			{Modifiers: []string{"SUPER"}, Key: "V", Dispatcher: "togglefloating", Enabled: true},
			{Modifiers: []string{"SUPER"}, Key: "R", Dispatcher: "reload", Enabled: true},
		},
	})
	if !strings.Contains(out, "SUPER,Q,spawn_shell,kitty") {
		t.Fatalf("expected SUPER,Q,spawn_shell,kitty, got: %s", out)
	}
	if !strings.Contains(out, "SUPER,C,killclient") {
		t.Fatalf("expected killclient, got: %s", out)
	}
	if !strings.Contains(out, "SUPER,F,togglefullscreen") {
		t.Fatalf("expected togglefullscreen, got: %s", out)
	}
	if !strings.Contains(out, "SUPER,V,togglefloating") {
		t.Fatalf("expected togglefloating, got: %s", out)
	}
	if !strings.Contains(out, "SUPER,R,reload_config") {
		t.Fatalf("expected reload_config, got: %s", out)
	}
}

func TestGenerateKeybindsMapsAmbxstDispatchers(t *testing.T) {
	g := &Generator{}
	out := g.GenerateKeybinds(ipc.ConfigKeybinds{
		Custom: []ipc.Keybind{
			{Modifiers: []string{"SUPER"}, Key: "C", Dispatcher: "killactive", Enabled: true},
			{Modifiers: []string{"SUPER"}, Key: "UP", Dispatcher: "movefocus", Argument: "u", Enabled: true},
			{Modifiers: []string{"SUPER", "SHIFT"}, Key: "LEFT", Dispatcher: "movewindow", Argument: "l", Enabled: true},
			{Modifiers: []string{"SUPER", "ALT"}, Key: "DOWN", Dispatcher: "resizeactive", Argument: "0 50", Enabled: true},
			{Modifiers: []string{"SUPER", "ALT", "CTRL"}, Key: "LEFT", Dispatcher: "layoutmsg", Argument: "swapcol l", Enabled: true},
			{Modifiers: []string{"SUPER", "SHIFT"}, Key: "V", Dispatcher: "togglespecialworkspace", Enabled: true},
			{Modifiers: []string{"SUPER", "ALT"}, Key: "V", Dispatcher: "movetoworkspace", Argument: "special", Enabled: true},
		},
	})
	for _, want := range []string{
		"SUPER,C,killclient",
		"SUPER,UP,focusdir,up",
		"SUPER+SHIFT,LEFT,exchange_client,left",
		"SUPER+ALT,DOWN,resizewin,+0,+50",
		"SUPER+ALT+CTRL,LEFT,exchange_client,left",
		"SUPER+SHIFT,V,toggle_special_tag",
		"SUPER+ALT,V,toggle_scratchpad",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing mapped Mango bind %q in:\n%s", want, out)
		}
	}
}

func TestGenerateKeybindsMapsWorkspaceOffsets(t *testing.T) {
	g := &Generator{}
	out := g.GenerateKeybinds(ipc.ConfigKeybinds{
		Custom: []ipc.Keybind{
			{Modifiers: []string{"SUPER"}, Key: "Z", Dispatcher: "workspace", Argument: "-1", Enabled: true},
			{Modifiers: []string{"SUPER"}, Key: "X", Dispatcher: "workspace", Argument: "+1", Enabled: true},
			{Modifiers: []string{"SUPER", "SHIFT"}, Key: "Z", Dispatcher: "workspace", Argument: "e-1", Enabled: true},
			{Modifiers: []string{"SUPER", "SHIFT"}, Key: "X", Dispatcher: "workspace", Argument: "e+1", Enabled: true},
			{Modifiers: []string{"SUPER"}, Key: "1", Dispatcher: "workspace", Argument: "1", Enabled: true},
		},
	})
	for _, want := range []string{
		"SUPER,Z,viewtoleft",
		"SUPER,X,viewtoright",
		"SUPER+SHIFT,Z,viewtoleft_have_client",
		"SUPER+SHIFT,X,viewtoright_have_client",
		"SUPER,1,view,1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing workspace bind %q in:\n%s", want, out)
		}
	}
}

func TestGenerateKeybindsSkipsDisabled(t *testing.T) {
	g := &Generator{}
	out := g.GenerateKeybinds(ipc.ConfigKeybinds{
		Custom: []ipc.Keybind{{Key: "X", Dispatcher: "exec", Argument: "true", Enabled: false}},
	})
	if strings.Contains(out, "X,exec") {
		t.Fatalf("expected disabled bind to be omitted, got: %s", out)
	}
}

func TestGenerateKeybindsSkipsUnsupportedMangoKeys(t *testing.T) {
	g := &Generator{}
	out := g.GenerateKeybinds(ipc.ConfigKeybinds{
		Custom: []ipc.Keybind{
			{Modifiers: []string{"SUPER"}, Key: "mouse_down", Dispatcher: "workspace", Argument: "e-1", Enabled: true},
			{Key: "switch:Lid Switch", Dispatcher: "exec", Argument: "loginctl lock-session", Enabled: true},
		},
	})
	if strings.Contains(out, "bind = SUPER,MOUSE_DOWN") || strings.Contains(out, "bind = none,SWITCH:LID SWITCH") {
		t.Fatalf("unsupported Mango keys were emitted: %s", out)
	}
	if !strings.Contains(out, `key "mouse_down" is not supported in Mango`) || !strings.Contains(out, `key "switch:Lid Switch" is not supported in Mango`) {
		t.Fatalf("unsupported Mango keys were not reported: %s", out)
	}
}

func TestGenerateWindowRules(t *testing.T) {
	g := &Generator{}
	out := g.GenerateWindowRules([]ipc.WindowRule{
		{Match: "class:firefox", Float: boolPtr(true), Pin: boolPtr(true)},
		{Match: "title:Settings", Fullscreen: boolPtr(true)},
	})
	if !strings.Contains(out, "window_rule=") {
		t.Fatalf("expected windowrule entries, got: %s", out)
	}
	if !strings.Contains(out, "app_id:firefox") {
		t.Fatalf("expected app_id match, got: %s", out)
	}
	if !strings.Contains(out, "is_floating:1") {
		t.Fatalf("expected is_floating:1, got: %s", out)
	}
	if !strings.Contains(out, "is_global:1") {
		t.Fatalf("expected is_global:1, got: %s", out)
	}
	if !strings.Contains(out, "is_fullscreen:1") {
		t.Fatalf("expected is_fullscreen:1, got: %s", out)
	}
}

func TestGenerateLayerRules(t *testing.T) {
	g := &Generator{}
	noBlur := true
	out := g.GenerateLayerRules([]ipc.LayerRule{{Namespace: "foo", NoAnim: boolPtr(true), Blur: &noBlur, NoShadow: boolPtr(true)}})
	if !strings.Contains(out, "layer_rule=layer_name:foo,no_animation:1,no_blur:0,no_shadow:1") {
		t.Fatalf("unexpected layer rule: %s", out)
	}
}

func TestGenerateStartupExecOnce(t *testing.T) {
	g := &Generator{}
	out := g.GenerateStartup([]string{"waybar"}, []string{"wl-paste --watch cliphist store"})
	if !strings.Contains(out, "exec_once = wl-paste --watch cliphist store") {
		t.Fatalf("expected exec-once, got: %s", out)
	}
	if !strings.Contains(out, "exec = waybar") {
		t.Fatalf("expected exec, got: %s", out)
	}
}

func TestGenerateKeybindsModifierSelfUsesReleaseBind(t *testing.T) {
	g := &Generator{}
	out := g.GenerateKeybinds(ipc.ConfigKeybinds{
		Custom: []ipc.Keybind{
			{Modifiers: []string{"SUPER"}, Key: "Super_L", Dispatcher: "exec", Argument: "ambxst run launcher", Enabled: true},
			{Modifiers: []string{"SUPER"}, Key: "Q", Dispatcher: "exec", Argument: "kitty", Enabled: true},
		},
	})
	if !strings.Contains(out, "bindr = SUPER,SUPER_L,spawn_shell,ambxst run launcher") {
		t.Fatalf("modifier-self bind should be a release bind, got: %s", out)
	}
	if strings.Contains(out, "bind = SUPER,SUPER_L") {
		t.Fatalf("modifier-self bind must not be a plain press bind, got: %s", out)
	}
	if !strings.Contains(out, "bind = SUPER,Q,spawn_shell,kitty") {
		t.Fatalf("ordinary bind should be kept, got: %s", out)
	}
	if strings.Contains(out, "keymon") {
		t.Fatalf("modifier-self binds are compositor-native now, got: %s", out)
	}
}

func TestMangoBindFlagsNormalizeWords(t *testing.T) {
	for raw, want := range map[string]string{
		"release":  "r",
		"lock":     "l",
		"pass":     "p",
		"conflict": "c",
		"sym":      "s",
		"rl":       "rl",
	} {
		if got := mangoBindFlags(raw); got != want {
			t.Errorf("mangoBindFlags(%q) = %q, want %q", raw, got, want)
		}
	}
}
