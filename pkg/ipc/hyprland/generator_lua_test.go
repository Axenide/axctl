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
