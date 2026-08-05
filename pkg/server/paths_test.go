package server

import (
	"path/filepath"
	"strings"
	"testing"

	"axctl/pkg/ipc/hyprland"
	"axctl/pkg/ipc/mango"
	"axctl/pkg/ipc/niri"
)

func TestResolveTargetPath(t *testing.T) {
	t.Setenv("HOME", "/home/tester")

	cases := []struct {
		name      string
		target    string
		configDir string
		want      string
	}{
		{"empty", "", "/etc/axctl", ""},
		{"tilde alone", "~", "/etc/axctl", "/home/tester"},
		{"tilde slash", "~/share/axctl", "/etc/axctl", "/home/tester/share/axctl"},
		{"absolute", "/var/lib/axctl/out", "/etc/axctl", "/var/lib/axctl/out"},
		{"relative", "hyprland.lua", "/etc/axctl", "/etc/axctl/hyprland.lua"},
		{"relative nested", "sub/dir/file.conf", "/etc/axctl", "/etc/axctl/sub/dir/file.conf"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveTargetPath(c.target, c.configDir)
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestPathsForCompositorWithTargetDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	dir := xdgConfigDir()

	hypr := &hyprland.Hyprland{}
	n := &niri.Niri{}
	m := &mango.Mango{}

	if got := PathsForCompositorWithTarget(hypr, "", "", "", "/anywhere"); !strings.HasSuffix(got.Primary(), "hypr/axctl.generated.conf") {
		t.Fatalf("hypr default primary = %q", got.Primary())
	} else if got.Primary() != filepath.Join(dir, "hypr", "axctl.generated.conf") {
		t.Fatalf("hypr default primary full = %q", got.Primary())
	}
	if got := PathsForCompositorWithTarget(n, "", "", "", "/anywhere"); !strings.HasSuffix(got.Primary(), "niri/axctl.generated.kdl") {
		t.Fatalf("niri default primary = %q", got.Primary())
	}
	if got := PathsForCompositorWithTarget(m, "", "", "", "/anywhere"); !strings.HasSuffix(got.Primary(), "mango/axctl.generated.conf") {
		t.Fatalf("mango default primary = %q", got.Primary())
	}
}

func TestPathsForCompositorWithTargetHyprlandLua(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	hypr := &hyprland.Hyprland{}

	got := PathsForCompositorWithTarget(hypr, "~/.local/share/ambxst/hyprland.lua", "", "", "/anywhere")
	if got.Alt() != "/home/tester/.local/share/ambxst/hyprland.lua" {
		t.Fatalf("lua alt = %q", got.Alt())
	}
	if got.Primary() != "/home/tester/.local/share/ambxst/hyprland.conf" {
		t.Fatalf("conf primary = %q", got.Primary())
	}
}

func TestPathsForCompositorWithTargetHyprlandConf(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	hypr := &hyprland.Hyprland{}

	got := PathsForCompositorWithTarget(hypr, "~/.local/share/ambxst/hyprland.conf", "", "", "/anywhere")
	if got.Primary() != "/home/tester/.local/share/ambxst/hyprland.conf" {
		t.Fatalf("conf primary = %q", got.Primary())
	}
	if got.Alt() != "/home/tester/.local/share/ambxst/hyprland.lua" {
		t.Fatalf("lua alt = %q", got.Alt())
	}
}

func TestPathsForCompositorWithTargetHyprlandNoExt(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	hypr := &hyprland.Hyprland{}

	got := PathsForCompositorWithTarget(hypr, "~/.local/share/ambxst/hyprland", "", "", "/anywhere")
	if got.Primary() != "/home/tester/.local/share/ambxst/hyprland.conf" {
		t.Fatalf("conf primary = %q", got.Primary())
	}
	if got.Alt() != "/home/tester/.local/share/ambxst/hyprland.lua" {
		t.Fatalf("lua alt = %q", got.Alt())
	}
}

func TestPathsForCompositorWithTargetRelative(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	hypr := &hyprland.Hyprland{}

	got := PathsForCompositorWithTarget(hypr, "./hyprland.lua", "", "", "/etc/axctl")
	if got.Alt() != "/etc/axctl/hyprland.lua" {
		t.Fatalf("relative lua = %q", got.Alt())
	}
	if got.Primary() != "/etc/axctl/hyprland.conf" {
		t.Fatalf("relative conf = %q", got.Primary())
	}
}

func TestPathsForCompositorWithTargetNiri(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	n := &niri.Niri{}

	got := PathsForCompositorWithTarget(n, "", "~/.local/share/ambxst/niri.kdl", "", "/anywhere")
	if got.Primary() != "/home/tester/.local/share/ambxst/niri.kdl" {
		t.Fatalf("niri primary = %q", got.Primary())
	}
	if got.Alt() != "" {
		t.Fatalf("niri alt = %q, want empty", got.Alt())
	}
}

func TestPathsForCompositorWithTargetMango(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	m := &mango.Mango{}

	got := PathsForCompositorWithTarget(m, "", "", "~/.local/share/ambxst/mango.conf", "/anywhere")
	if got.Primary() != "/home/tester/.local/share/ambxst/mango.conf" {
		t.Fatalf("mango primary = %q", got.Primary())
	}
}

func TestPathsForCompositorWithTargetWrongFieldIgnored(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	t.Setenv("HOME", "/home/tester")
	hypr := &hyprland.Hyprland{}

	got := PathsForCompositorWithTarget(hypr, "", "~/.local/share/ambxst/niri.kdl", "", "/anywhere")
	if !strings.HasSuffix(got.Primary(), "hypr/axctl.generated.conf") {
		t.Fatalf("niri target on hypr should be ignored, got %q", got.Primary())
	}
}
