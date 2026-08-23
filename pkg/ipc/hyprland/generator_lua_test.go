package hyprland

import (
	"strings"
	"testing"

	"axctl/pkg/ipc"
)

func TestDispatcherToLuaMovefocusMonocle(t *testing.T) {
	cases := []struct {
		arg      string
		wantAny  []string // substrings that must appear in the generated Lua
		wantNone []string // substrings that must NOT appear
	}{
		{
			arg: "u",
			wantAny: []string{
				`layout == "scrolling"`,
				`layout == "monocle"`,
				`hl.dsp.layout("cyclenext")`,
				`hl.dsp.focus({ direction = "u" })`,
			},
		},
		{
			arg: "r",
			wantAny: []string{
				`hl.dsp.layout("cyclenext")`,
			},
		},
		{
			arg: "d",
			wantAny: []string{
				`hl.dsp.layout("cycleprev")`,
			},
		},
		{
			arg: "l",
			wantAny: []string{
				`hl.dsp.layout("cycleprev")`,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.arg, func(t *testing.T) {
			lua := dispatcherToLua("movefocus", c.arg)
			for _, want := range c.wantAny {
				if !strings.Contains(lua, want) {
					t.Errorf("movefocus %q: missing %q in:\n%s", c.arg, want, lua)
				}
			}
		})
	}
}

func TestDispatcherToLuaMovefocusKeepsScrollingBranch(t *testing.T) {
	// The original "focus <dir>" layoutmsg path for scrolling must
	// remain intact after the monocle branch was added.
	lua := dispatcherToLua("movefocus", "u")
	if !strings.Contains(lua, `hl.dsp.layout("focus u")`) {
		t.Errorf("scrolling branch lost; got:\n%s", lua)
	}
}

func TestDispatcherToLuaMovewindowMonocleCycles(t *testing.T) {
	cases := []struct {
		arg     string
		wantAny string
	}{
		{"u", `hl.dsp.layout("cyclenext")`},
		{"r", `hl.dsp.layout("cyclenext")`},
		{"d", `hl.dsp.layout("cycleprev")`},
		{"l", `hl.dsp.layout("cycleprev")`},
	}
	for _, c := range cases {
		t.Run(c.arg, func(t *testing.T) {
			lua := dispatcherToLua("movewindow", c.arg)
			if !strings.Contains(lua, c.wantAny) {
				t.Errorf("movewindow %q: missing %q in:\n%s", c.arg, c.wantAny, lua)
			}
			if !strings.Contains(lua, `layout == "monocle"`) {
				t.Errorf("movewindow %q: missing monocle branch in:\n%s", c.arg, lua)
			}
		})
	}
}

func TestDispatcherToLuaMovewindowNonMonocleStaysDirect(t *testing.T) {
	lua := dispatcherToLua("movewindow", "u")
	if !strings.Contains(lua, `hl.dsp.window.move({ direction = "u" })`) {
		t.Errorf("non-monocle branch lost; got:\n%s", lua)
	}
}

func TestDispatcherToLuaMovewindowDragUnchanged(t *testing.T) {
	lua := dispatcherToLua("movewindow", "")
	want := "hl.dsp.window.drag()"
	if lua != want {
		t.Errorf("drag variant regressed: got %q, want %q", lua, want)
	}
}

// Each Lua section generator should be banner-free. The orchestrator in
// pkg/server writes the banner exactly once at the top of the file.
func TestLuaSectionsAreBannerFree(t *testing.T) {
	gen := NewLuaGenerator()
	banner := "-- ▄    ▄▄▄  ▄▄ ▄▄  ▄▄▄▄ ▄▄▄▄▄▄ ▄▄"

	sections := []string{
		gen.GenerateAppearanceLua(ipc.ConfigAppearance{}),
		gen.GenerateKeybindsLua(ipc.ConfigKeybinds{}),
		gen.GenerateWindowRulesLua(nil),
		gen.GenerateLayerRulesLua(nil),
	}
	for _, sec := range sections {
		if strings.Contains(sec, banner) {
			t.Errorf("section generator leaked the banner; section:\n%s", sec)
		}
	}

	if got := gen.GenerateStartupLua([]string{"notify-send hi"}, []string{"ambxst"}); strings.Contains(got, banner) {
		t.Errorf("GenerateStartupLua leaked the banner:\n%s", got)
	}
	if got := gen.GenerateStartupLua(nil, nil); strings.Contains(got, banner) {
		t.Errorf("empty GenerateStartupLua leaked the banner:\n%s", got)
	}
}

// Workspaces animation style defaults to slidefade, follows the
// Animations.WorkspaceStyle override when set. Used by ambxst to
// switch between slidefade (vertical bar) and slidefadevert
// (horizontal bar) without a Timer-based live patch.
func TestLuaAppearanceWorkspaceStyleDefault(t *testing.T) {
	gen := NewLuaGenerator()
	enabled := true
	out := gen.GenerateAppearanceLua(ipc.ConfigAppearance{
		Animations: &ipc.Animations{Enabled: &enabled},
	})
	if !strings.Contains(out, `leaf = "workspaces"`) {
		t.Fatalf("missing workspaces animation, got:\n%s", out)
	}
	if !strings.Contains(out, `"slidefade 20%"`) {
		t.Fatalf("expected default slidefade 20%% style, got:\n%s", out)
	}
	if strings.Contains(out, `slidefadevert`) {
		t.Fatalf("unexpected slidefadevert with no WorkspaceStyle, got:\n%s", out)
	}
}

func TestLuaAppearanceWorkspaceStyleOverride(t *testing.T) {
	gen := NewLuaGenerator()
	vert := "slidefadevert 20%"
	enabled := true
	out := gen.GenerateAppearanceLua(ipc.ConfigAppearance{
		Animations: &ipc.Animations{
			Enabled:        &enabled,
			WorkspaceStyle: &vert,
		},
	})
	if !strings.Contains(out, `"slidefadevert 20%"`) {
		t.Fatalf("expected slidefadevert 20%% override, got:\n%s", out)
	}
	if strings.Contains(out, `"slidefade 20%"`) {
		t.Fatalf("default slidefade leaked through with override set, got:\n%s", out)
	}
}

func TestLuaAppearanceWorkspaceStyleEmptyFallsBack(t *testing.T) {
	gen := NewLuaGenerator()
	empty := ""
	enabled := true
	out := gen.GenerateAppearanceLua(ipc.ConfigAppearance{
		Animations: &ipc.Animations{
			Enabled:        &enabled,
			WorkspaceStyle: &empty,
		},
	})
	if !strings.Contains(out, `"slidefade 20%"`) {
		t.Fatalf("empty WorkspaceStyle should fall back to default, got:\n%s", out)
	}
}
