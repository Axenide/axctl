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
	if !strings.Contains(out, "gappih = 8") || !strings.Contains(out, "gappiv = 8") {
		t.Fatalf("expected inner gaps, got: %s", out)
	}
	if !strings.Contains(out, "gappoh = 4") || !strings.Contains(out, "gappov = 4") {
		t.Fatalf("expected outer gaps, got: %s", out)
	}
	if !strings.Contains(out, "borderpx = 3") {
		t.Fatalf("expected borderpx, got: %s", out)
	}
	if !strings.Contains(out, "focuscolor = 0xff8800ff") {
		t.Fatalf("expected focuscolor 0xff8800ff, got: %s", out)
	}
	if !strings.Contains(out, "bordercolor = 0x333333ff") {
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
	if !strings.Contains(out, "SUPER,Q,spawn,kitty") {
		t.Fatalf("expected SUPER,Q,spawn,kitty, got: %s", out)
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

func TestGenerateKeybindsSkipsDisabled(t *testing.T) {
	g := &Generator{}
	out := g.GenerateKeybinds(ipc.ConfigKeybinds{
		Custom: []ipc.Keybind{{Key: "X", Dispatcher: "exec", Argument: "true", Enabled: false}},
	})
	if strings.Contains(out, "X,exec") {
		t.Fatalf("expected disabled bind to be omitted, got: %s", out)
	}
}

func TestGenerateWindowRules(t *testing.T) {
	g := &Generator{}
	out := g.GenerateWindowRules([]ipc.WindowRule{
		{Match: "class:firefox", Float: boolPtr(true), Pin: boolPtr(true)},
		{Match: "title:Settings", Fullscreen: boolPtr(true)},
	})
	if !strings.Contains(out, "windowrule = ") {
		t.Fatalf("expected windowrule entries, got: %s", out)
	}
	if !strings.Contains(out, "appid:firefox") {
		t.Fatalf("expected appid match, got: %s", out)
	}
	if !strings.Contains(out, "isfloating:1") {
		t.Fatalf("expected isfloating:1, got: %s", out)
	}
	if !strings.Contains(out, "isglobal:1") {
		t.Fatalf("expected isglobal:1, got: %s", out)
	}
	if !strings.Contains(out, "isfullscreen:1") {
		t.Fatalf("expected isfullscreen:1, got: %s", out)
	}
}

func TestGenerateLayerRulesIsNoOp(t *testing.T) {
	g := &Generator{}
	out := g.GenerateLayerRules([]ipc.LayerRule{{Namespace: "foo"}})
	if !strings.Contains(out, "not supported") {
		t.Fatalf("expected unsupported comment, got: %s", out)
	}
}

func TestGenerateStartupExecOnce(t *testing.T) {
	g := &Generator{}
	out := g.GenerateStartup([]string{"waybar"}, []string{"wl-paste --watch cliphist store"})
	if !strings.Contains(out, "exec-once = wl-paste --watch cliphist store") {
		t.Fatalf("expected exec-once, got: %s", out)
	}
	if !strings.Contains(out, "exec = waybar") {
		t.Fatalf("expected exec, got: %s", out)
	}
}
