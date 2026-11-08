package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWorkspaceLayoutOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "axctl.toml")
	content := `[general]
layout = "scroller"

[general.workspace_layouts]
"HDMI-A-1:1" = "tile"
"HDMI-A-1:2" = "scroller"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.ToIPCConfig().Appearance
	if got.Layout == nil || *got.Layout != "scroller" {
		t.Fatalf("default layout = %v, want scroller", got.Layout)
	}
	if got.WorkspaceLayouts["HDMI-A-1:1"] != "tile" || got.WorkspaceLayouts["HDMI-A-1:2"] != "scroller" {
		t.Fatalf("workspace layouts = %v", got.WorkspaceLayouts)
	}
}

func TestLoadEmptyWorkspaceLayoutTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "axctl.toml")
	if err := os.WriteFile(path, []byte("[general]\nworkspace_layouts_enabled = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	appearance := loaded.ToIPCConfig().Appearance
	if !appearance.WorkspaceLayoutsEnabled || appearance.WorkspaceLayouts == nil {
		t.Fatalf("empty workspace layout setting was lost during loading: %+v", appearance)
	}
}
