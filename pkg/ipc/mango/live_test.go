package mango

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"axctl/pkg/ipc"
)

func TestMangoLiveReadOnly(t *testing.T) {
	if os.Getenv("AXCTL_MANGO_LIVE") != "1" {
		t.Skip("set AXCTL_MANGO_LIVE=1 to query a running Mango session")
	}
	m, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if _, err := m.ListWindows(); err != nil {
		t.Fatalf("ListWindows: %v", err)
	}
	if _, err := m.ActiveWindow(); err != nil {
		t.Fatalf("ActiveWindow: %v", err)
	}
	workspaces, err := m.ListWorkspaces()
	if err != nil || len(workspaces) == 0 {
		t.Fatalf("ListWorkspaces: got %d entries, error %v", len(workspaces), err)
	}
	activeWorkspace, err := m.ActiveWorkspace()
	if err != nil {
		t.Fatalf("ActiveWorkspace: %v", err)
	}
	if layout, ok := activeWorkspace.Metadata["layout"].(string); !ok || layout == "" {
		t.Fatalf("active Mango workspace did not expose a normalized layout name: %+v", activeWorkspace)
	}
	monitors, err := m.ListMonitors()
	if err != nil || len(monitors) == 0 {
		t.Fatalf("ListMonitors: got %d entries, error %v", len(monitors), err)
	}
	if _, err := m.ListLayouts(); err != nil {
		t.Fatalf("ListLayouts: %v", err)
	}
	if _, _, err := m.GetCursorPosition(); err != nil {
		t.Fatalf("GetCursorPosition: %v", err)
	}
	if _, err := m.GetCapabilities(); err != nil {
		t.Fatalf("GetCapabilities: %v", err)
	}
	events, err := m.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	select {
	case event, ok := <-events:
		if !ok || event.Type == "" {
			t.Fatalf("invalid initial watch event: %+v", event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for initial Mango watch state")
	}
}

func TestMangoLiveGeneratedConfigParses(t *testing.T) {
	if os.Getenv("AXCTL_MANGO_LIVE") != "1" {
		t.Skip("set AXCTL_MANGO_LIVE=1 to validate against Mango's parser")
	}
	generated := &Generator{}
	config := ipc.ConfigUniversal{
		Appearance: ipc.ConfigAppearance{
			Gaps:       &ipc.Gaps{Inner: liveInt(5), Outer: liveInt(10)},
			Border:     &ipc.Border{Width: liveInt(2), ActiveColor: liveString("#ff8800"), Rounding: liveInt(6)},
			Opacity:    &ipc.Opacity{Active: liveFloat(1), Inactive: liveFloat(0.9)},
			Blur:       &ipc.Blur{Enabled: liveBool(true), Size: liveInt(5), Passes: liveInt(2)},
			Shadow:     &ipc.Shadow{Enabled: liveBool(true), Size: liveInt(12), Color: liveString("#000000aa")},
			Animations: &ipc.Animations{Enabled: liveBool(true), WorkspaceStyle: liveString("slidefadevert 20%")},
			WorkspaceLayouts: map[string]string{
				"HDMI-A-1:1": "scroller",
				"HDMI-A-1:2": "tile",
			},
		},
		Keybinds: ipc.ConfigKeybinds{Custom: []ipc.Keybind{
			{Modifiers: []string{"SUPER"}, Key: "Q", Dispatcher: "exec", Argument: "true", Enabled: true},
			{Modifiers: []string{"SUPER"}, Key: "Z", Dispatcher: "workspace", Argument: "-1", Enabled: true},
			{Modifiers: []string{"SUPER"}, Key: "X", Dispatcher: "workspace", Argument: "+1", Enabled: true},
			{Modifiers: []string{"SUPER", "SHIFT"}, Key: "Z", Dispatcher: "workspace", Argument: "e-1", Enabled: true},
			{Modifiers: []string{"SUPER", "SHIFT"}, Key: "X", Dispatcher: "workspace", Argument: "e+1", Enabled: true},
			{Modifiers: []string{"SUPER"}, Key: "Up", Dispatcher: "movefocus", Argument: "u", Enabled: true},
			{Modifiers: []string{"SUPER", "SHIFT"}, Key: "Left", Dispatcher: "movewindow", Argument: "l", Enabled: true},
			{Modifiers: []string{"SUPER", "ALT"}, Key: "Down", Dispatcher: "resizeactive", Argument: "0 50", Enabled: true},
			{Modifiers: []string{"SUPER", "SHIFT"}, Key: "SPACE", Dispatcher: "layoutmsg", Argument: "colresize +conf", Enabled: true},
			{Modifiers: []string{"SUPER", "ALT"}, Key: "Right", Dispatcher: "layoutmsg", Argument: "colresize +0.1", Enabled: true},
			{Modifiers: []string{"SUPER", "ALT"}, Key: "Left", Dispatcher: "layoutmsg", Argument: "colresize -0.1", Enabled: true},
			{Modifiers: []string{"SUPER", "ALT", "CTRL"}, Key: "Left", Dispatcher: "layoutmsg", Argument: "swapcol l", Enabled: true},
			{Modifiers: []string{"SUPER", "SHIFT"}, Key: "V", Dispatcher: "togglespecialworkspace", Enabled: true},
			{Modifiers: []string{"SUPER"}, Key: "mouse_down", Dispatcher: "workspace", Argument: "e-1", Enabled: true},
			{Key: "switch:Lid Switch", Dispatcher: "exec", Argument: "true", Enabled: true},
		}},
		WindowRules: []ipc.WindowRule{{
			Match: "class:foot", Float: liveBool(true), NoBlur: liveBool(true), Size: liveString("800x600"),
		}},
		LayerRules: []ipc.LayerRule{{Namespace: "axctl-live-test", NoAnim: liveBool(true), Blur: liveBool(false)}},
		ExecOnce:   []string{"true"},
	}
	var contents strings.Builder
	contents.WriteString(generated.GenerateStartup(config.Exec, config.ExecOnce))
	contents.WriteString(generated.GenerateAppearance(config.Appearance))
	contents.WriteString(generated.GenerateKeybinds(config.Keybinds))
	contents.WriteString(generated.GenerateWindowRules(config.WindowRules))
	contents.WriteString(generated.GenerateLayerRules(config.LayerRules))
	path := filepath.Join(t.TempDir(), "axctl.generated.conf")
	if err := os.WriteFile(path, []byte(contents.String()), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("mango", "-c", path, "-p")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("mango config parser rejected generated file: %v\n%s\n%s", err, fmt.Sprintf("config: %s", path), output)
	}
	if strings.Contains(string(output), "[ERROR]") || strings.Contains(string(output), "Unknown key:") {
		t.Fatalf("mango config parser reported errors for generated file:\n%s", output)
	}
}

func TestMangoLiveReversibleDispatches(t *testing.T) {
	if os.Getenv("AXCTL_MANGO_LIVE") != "1" {
		t.Skip("set AXCTL_MANGO_LIVE=1 to exercise Mango dispatches")
	}
	m, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	windows, err := m.ListWindows()
	if err != nil {
		t.Fatal(err)
	}
	activeID, err := m.ActiveWindow()
	if err != nil {
		t.Fatal(err)
	}
	if activeID != "" {
		if err := m.FocusWindow(activeID); err != nil {
			t.Fatalf("refocus existing active window: %v", err)
		}
	}
	for _, window := range windows {
		if !window.IsFocused {
			continue
		}
		maximized, _ := window.Metadata["maximized"].(bool)
		pinned, _ := window.Metadata["global"].(bool)
		fakeFullscreen, _ := window.Metadata["fake_fullscreen"].(bool)
		for _, action := range []func() error{
			func() error { return m.SetFullscreen(window.ID, window.IsFullscreen) },
			func() error { return m.SetMaximized(window.ID, maximized) },
			func() error { return m.PinWindow(window.ID, pinned) },
			func() error { return m.SetFakeFullscreen(window.ID, fakeFullscreen) },
		} {
			if err := action(); err != nil {
				t.Fatal(err)
			}
		}
		break
	}
	if activeID != "" {
		activeWorkspace, err := m.ActiveWorkspace()
		if err != nil {
			t.Fatal(err)
		}
		originalLayout, _ := activeWorkspace.Metadata["layout"].(string)
		if originalLayout != "scroller" {
			if err := m.SetLayout("scroller"); err != nil {
				t.Fatalf("temporarily select scroller layout: %v", err)
			}
			defer func() {
				if err := m.SetLayout(originalLayout); err != nil {
					t.Errorf("restore original Mango layout %q: %v", originalLayout, err)
				}
			}()
		}
		for _, change := range []string{"+0.1", "-0.1"} {
			if err := m.SetLayoutProperty(activeID, "scroller_proportion", change); err != nil {
				t.Fatalf("adjust scroller proportion by %s: %v", change, err)
			}
		}
	}

	monitors, err := m.ListMonitors()
	if err != nil || len(monitors) == 0 {
		t.Fatalf("ListMonitors: %+v, %v", monitors, err)
	}
	activeTags, _ := monitors[0].Metadata["active_tags"].([]int)
	for _, tag := range activeTags {
		if tag == 0 {
			t.Skip("tag 0 is currently reported active; overview and special-tag state are indistinguishable")
		}
	}
	if len(activeTags) == 0 {
		t.Skip("Mango did not report active tags for the current monitor")
	}
	toggled := false
	defer func() {
		if toggled {
			_ = m.ToggleOverview()
		}
	}()
	if err := m.ToggleOverview(); err != nil {
		t.Fatalf("enter overview: %v", err)
	}
	toggled = true
	time.Sleep(150 * time.Millisecond)
	monitors, err = m.ListMonitors()
	if err != nil {
		t.Fatalf("ListMonitors after opening overview: %v", err)
	}
	openedTags, _ := monitors[0].Metadata["active_tags"].([]int)
	if !containsTag(openedTags, 0) {
		if err := m.ToggleOverview(); err == nil {
			toggled = false
		}
		t.Fatalf("toggleoverview did not expose Mango's overview tag marker: %v", openedTags)
	}
	if err := m.ToggleOverview(); err != nil {
		t.Fatalf("leave overview: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	monitors, err = m.ListMonitors()
	if err != nil {
		t.Fatalf("ListMonitors after closing overview: %v", err)
	}
	closedTags, _ := monitors[0].Metadata["active_tags"].([]int)
	if containsTag(closedTags, 0) {
		t.Fatalf("overview remained active after second toggle: %v", closedTags)
	}
	if !reflect.DeepEqual(activeTags, closedTags) {
		t.Fatalf("active tags after toggling overview twice = %v, want %v", closedTags, activeTags)
	}
	toggled = false
}

func TestMangoLiveLayoutsAreStoredPerTag(t *testing.T) {
	if os.Getenv("AXCTL_MANGO_LIVE") != "1" {
		t.Skip("set AXCTL_MANGO_LIVE=1 to test Mango per-tag layout state")
	}
	m, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	originalWorkspace, err := m.ActiveWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	originalWindow, err := m.ActiveWindow()
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := m.ListWorkspaces()
	if err != nil {
		t.Fatal(err)
	}
	var target *ipc.Workspace
	for i := range workspaces {
		workspace := &workspaces[i]
		if workspace.MonitorID != originalWorkspace.MonitorID || workspace.ID == originalWorkspace.ID || !workspace.IsEmpty {
			continue
		}
		if workspace.Metadata["special"] == true {
			continue
		}
		target = workspace
		break
	}
	if target == nil {
		t.Skip("no empty non-special tag is available for an isolated per-tag layout check")
	}
	targetLayout, _ := target.Metadata["layout"].(string)
	if targetLayout == "" {
		t.Skip("Mango did not report a named layout for the target tag")
	}
	newLayout := "scroller"
	if targetLayout == newLayout {
		newLayout = "tile"
	}
	changedTagLayout := false
	switchedWorkspace := false
	defer func() {
		if changedTagLayout {
			if m.SwitchWorkspace(target.ID) == nil {
				_ = m.SetLayout(targetLayout)
			}
		}
		if switchedWorkspace {
			_ = m.SwitchWorkspace(originalWorkspace.ID)
			if originalWindow != "" {
				_ = m.FocusWindow(originalWindow)
			}
		}
	}()
	if err := m.SwitchWorkspace(target.ID); err != nil {
		t.Fatalf("switch to target tag: %v", err)
	}
	switchedWorkspace = true
	if err := m.SetLayout(newLayout); err != nil {
		t.Fatalf("set target tag layout: %v", err)
	}
	changedTagLayout = true
	workspaces, err = m.ListWorkspaces()
	if err != nil {
		t.Fatal(err)
	}
	layouts := make(map[string]string, len(workspaces))
	for _, workspace := range workspaces {
		if name, ok := workspace.Metadata["layout"].(string); ok {
			layouts[workspace.ID] = name
		}
	}
	if layouts[target.ID] != newLayout {
		t.Fatalf("target tag layout = %q, want %q", layouts[target.ID], newLayout)
	}
	if layouts[originalWorkspace.ID] != originalWorkspace.Metadata["layout"] {
		t.Fatalf("changing %s changed active tag %s layout: %+v", target.ID, originalWorkspace.ID, layouts)
	}
	if err := m.SetLayout(targetLayout); err != nil {
		t.Fatalf("restore target tag layout: %v", err)
	}
	changedTagLayout = false
	if err := m.SwitchWorkspace(originalWorkspace.ID); err != nil {
		t.Fatalf("restore original workspace: %v", err)
	}
	switchedWorkspace = false
	if originalWindow != "" {
		if err := m.FocusWindow(originalWindow); err != nil {
			t.Fatalf("restore original focused window: %v", err)
		}
	}
}

func liveBool(value bool) *bool        { return &value }
func liveInt(value int) *int           { return &value }
func liveFloat(value float64) *float64 { return &value }
func liveString(value string) *string  { return &value }

func containsTag(tags []int, target int) bool {
	for _, tag := range tags {
		if tag == target {
			return true
		}
	}
	return false
}
