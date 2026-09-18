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
