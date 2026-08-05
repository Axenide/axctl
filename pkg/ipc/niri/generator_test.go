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
