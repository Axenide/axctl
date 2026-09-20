package hyprland

import (
	"strings"
	"testing"

	"axctl/pkg/ipc"
)

func TestGenerateLayerRulesSkipsNiriOnlyProps(t *testing.T) {
	g := &Generator{}
	yes := true
	out := g.GenerateLayerRules([]ipc.LayerRule{
		{Namespace: "^ambxst:wallpaper$", PlaceWithinBackdrop: &yes},
	})
	if strings.Contains(out, "layerrule") {
		t.Fatalf("expected no layerrule for a niri-only property, got: %s", out)
	}
}

func TestGenerateLayerRulesKnownProps(t *testing.T) {
	g := &Generator{}
	yes := true
	out := g.GenerateLayerRules([]ipc.LayerRule{
		{Namespace: "quickshell", Blur: &yes, NoAnim: &yes},
	})
	if !strings.Contains(out, "layerrule") || !strings.Contains(out, "blur on") {
		t.Fatalf("expected layerrule with blur, got: %s", out)
	}
}

func TestGenerateKeybindsSkipsModifierSelf(t *testing.T) {
	g := &Generator{}
	out := g.GenerateKeybinds(ipc.ConfigKeybinds{
		Custom: []ipc.Keybind{
			{Modifiers: []string{"SUPER"}, Key: "Super_L", Dispatcher: "exec", Argument: "ambxst run launcher", Enabled: true},
			{Modifiers: []string{"SUPER"}, Key: "Q", Dispatcher: "exec", Argument: "kitty", Enabled: true},
		},
	})
	if strings.Contains(out, "bind = SUPER, Super_L") {
		t.Fatalf("modifier-self bind should be skipped, got: %s", out)
	}
	if !strings.Contains(out, "bind = SUPER, Q, exec, kitty") {
		t.Fatalf("ordinary bind should be kept, got: %s", out)
	}
	if !strings.Contains(out, "handled by the axctl keymon monitor") {
		t.Fatalf("expected skip comment, got: %s", out)
	}
}
