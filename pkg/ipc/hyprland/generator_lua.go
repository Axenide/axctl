package hyprland

import (
	"fmt"
	"strings"

	"axctl/pkg/ipc"
)

type LuaGenerator struct{}

func NewLuaGenerator() *LuaGenerator {
	return &LuaGenerator{}
}

func luaBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func (g *LuaGenerator) GenerateAppearanceLua(config ipc.ConfigAppearance) string {
	var b strings.Builder

	needsGeneral := (config.Gaps != nil) || (config.Border != nil) || (config.Layout != nil && *config.Layout != "")
	needsDecoration := (config.Border != nil && config.Border.Rounding != nil) || (config.Opacity != nil) || (config.Shadow != nil) || (config.Blur != nil)

	if needsGeneral {
		b.WriteString("hl.config({\n")
		b.WriteString("    general = {\n")
		if config.Gaps != nil {
			if config.Gaps.Inner != nil {
				b.WriteString(fmt.Sprintf("        gaps_in = %d,\n", *config.Gaps.Inner))
			}
			if config.Gaps.Outer != nil {
				b.WriteString(fmt.Sprintf("        gaps_out = %d,\n", *config.Gaps.Outer))
			}
		}
		if config.Border != nil {
			if config.Border.Width != nil {
				b.WriteString(fmt.Sprintf("        border_size = %d,\n", *config.Border.Width))
			}
			hasActive := config.Border.ActiveColor != nil
			hasInactive := config.Border.InactiveColor != nil
			if hasActive || hasInactive {
				b.WriteString("        col = {\n")
				if hasActive {
					colors, angle := parseColorString(*config.Border.ActiveColor)
					if colors != "" {
						if angle != "" {
							angleNum := strings.TrimSuffix(angle, "deg")
							colorParts := strings.Fields(colors)
							quoted := make([]string, len(colorParts))
							for i, cp := range colorParts {
								quoted[i] = fmt.Sprintf("%q", cp)
							}
							colorList := strings.Join(quoted, ", ")
							b.WriteString(fmt.Sprintf("            active_border = { colors = {%s}, angle = %s },\n", colorList, angleNum))
						} else {
							b.WriteString(fmt.Sprintf("            active_border = \"%s\",\n", colors))
						}
					}
				}
				if hasInactive {
					colors, _ := parseColorString(*config.Border.InactiveColor)
					if colors != "" {
						b.WriteString(fmt.Sprintf("            inactive_border = \"%s\",\n", strings.TrimSuffix(colors, " ")))
					}
				}
				b.WriteString("        },\n")
			}
		}
		if config.Layout != nil && *config.Layout != "" {
			b.WriteString(fmt.Sprintf("        layout = \"%s\",\n", *config.Layout))
		}
		b.WriteString("    },\n")
	}

	if needsDecoration {
		if !needsGeneral {
			b.WriteString("hl.config({\n")
		}
		b.WriteString("    decoration = {\n")
		if config.Border != nil && config.Border.Rounding != nil {
			b.WriteString(fmt.Sprintf("        rounding = %d,\n", *config.Border.Rounding))
		}
		if config.Opacity != nil {
			if config.Opacity.Active != nil {
				b.WriteString(fmt.Sprintf("        active_opacity = %.2f,\n", *config.Opacity.Active))
			}
			if config.Opacity.Inactive != nil {
				b.WriteString(fmt.Sprintf("        inactive_opacity = %.2f,\n", *config.Opacity.Inactive))
			}
		}
		if config.Shadow != nil && config.Shadow.Enabled != nil {
			b.WriteString("        shadow = {\n")
			b.WriteString(fmt.Sprintf("            enabled = %s,\n", luaBool(*config.Shadow.Enabled)))
			if config.Shadow.Size != nil {
				b.WriteString(fmt.Sprintf("            range = %d,\n", *config.Shadow.Size))
			}
			if config.Shadow.Color != nil {
				colorStr, _ := parseColorString(*config.Shadow.Color)
				if colorStr != "" {
					b.WriteString(fmt.Sprintf("            color = \"%s\",\n", colorStr))
				}
			}
			b.WriteString("        },\n")
		}
		if config.Blur != nil {
			b.WriteString("        blur = {\n")
			if config.Blur.Enabled != nil {
				b.WriteString(fmt.Sprintf("            enabled = %s,\n", luaBool(*config.Blur.Enabled)))
			}
			if config.Blur.Size != nil {
				b.WriteString(fmt.Sprintf("            size = %d,\n", *config.Blur.Size))
			}
			if config.Blur.Passes != nil {
				b.WriteString(fmt.Sprintf("            passes = %d,\n", *config.Blur.Passes))
			}
			b.WriteString("        },\n")
		}
		b.WriteString("    },\n")
	}

	if needsGeneral || needsDecoration {
		b.WriteString("})\n\n")
	}

	if config.Animations != nil && config.Animations.Enabled != nil {
		b.WriteString("hl.config({\n")
		b.WriteString(fmt.Sprintf("    animations = { enabled = %s },\n", luaBool(*config.Animations.Enabled)))
		b.WriteString("})\n\n")

		if *config.Animations.Enabled {
			b.WriteString("hl.curve(\"myBezier\", { type = \"bezier\", points = { {0.4, 0.0}, {0.2, 1.0} } })\n\n")
			b.WriteString("hl.animation({ leaf = \"windows\", enabled = true, speed = 2.5, bezier = \"myBezier\", style = \"popin 80%\" })\n")
			b.WriteString("hl.animation({ leaf = \"border\", enabled = true, speed = 2.5, bezier = \"myBezier\" })\n")
			b.WriteString("hl.animation({ leaf = \"fade\", enabled = true, speed = 2.5, bezier = \"myBezier\" })\n")
			workspaceStyle := "slidefade 20%"
			if config.Animations.WorkspaceStyle != nil && *config.Animations.WorkspaceStyle != "" {
				workspaceStyle = *config.Animations.WorkspaceStyle
			}
			b.WriteString(fmt.Sprintf("hl.animation({ leaf = \"workspaces\", enabled = true, speed = 2.5, bezier = \"myBezier\", style = %q })\n", workspaceStyle))
		}
	}

	return b.String()
}

func (g *LuaGenerator) GenerateKeybindsLua(config ipc.ConfigKeybinds) string {
	var b strings.Builder
	b.WriteString("-- Generated by axctl LuaGenerator (Keybinds)\n")
	b.WriteString("-- Do not edit manually!\n\n")

	addBind := func(kb ipc.Keybind, comment string) {
		if !kb.Enabled || kb.Key == "" {
			return
		}
		mods := strings.Join(kb.Modifiers, " + ")
		key := kb.Key
		dispatcher := kb.Dispatcher
		arg := kb.Argument

		keyStr := key
		if mods != "" {
			keyStr = mods + " + " + key
		}

		isMouse := strings.HasPrefix(strings.ToLower(key), "mouse:")
		if isMouse {
			keyStr = mods + " + " + key
		}

		if dispatcher == "" || dispatcher == "exec" {
			if arg != "" {
				b.WriteString(fmt.Sprintf("hl.bind(%s, hl.dsp.exec_cmd(%q))\n", luaQuote(keyStr), arg))
			} else {
				b.WriteString(fmt.Sprintf("hl.bind(%s, hl.dsp.exec_cmd(%q))\n", luaQuote(keyStr), ""))
			}
			return
		}

		actionLua := dispatcherToLua(dispatcher, arg)
		if isMouse {
			b.WriteString(fmt.Sprintf("hl.bind(%s, %s, { mouse = true })\n", luaQuote(keyStr), actionLua))
		} else {
			flags := bindFlagsToLua(kb.Flags)
			if flags != "" {
				b.WriteString(fmt.Sprintf("hl.bind(%s, %s, { %s })\n", luaQuote(keyStr), actionLua, flags))
			} else {
				b.WriteString(fmt.Sprintf("hl.bind(%s, %s)\n", luaQuote(keyStr), actionLua))
			}
		}
	}

	if config.Ambxst != nil {
		if config.Ambxst.System != nil {
			for name, kb := range config.Ambxst.System {
				addBind(kb, fmt.Sprintf("Ambxst System: %s", name))
			}
		}
		if config.Ambxst.Binds != nil {
			for name, kb := range config.Ambxst.Binds {
				addBind(kb, fmt.Sprintf("Ambxst: %s", name))
			}
		}
	}

	if config.Custom != nil {
		for i, kb := range config.Custom {
			addBind(kb, fmt.Sprintf("Custom Bind %d", i))
		}
	}

	return b.String()
}

func luaQuote(s string) string {
	return fmt.Sprintf("%q", s)
}

func bindFlagsToLua(flags string) string {
	if flags == "" {
		return ""
	}
	var parts []string
	for _, f := range strings.Split(flags, "") {
		switch strings.ToLower(f) {
		case "l":
			parts = append(parts, "locked = true")
		case "e":
			parts = append(parts, "release = true")
		case "n":
			parts = append(parts, "non_consuming = true")
		case "m":
			parts = append(parts, "mouse = true")
		case "r":
			parts = append(parts, "repeating = true")
		}
	}
	return strings.Join(parts, ", ")
}

func isScrollingLayoutMsg(arg string) bool {
	prefixes := []string{"focus ", "movewindowto ", "colresize", "promote", "togglefit", "swapcol ", "movecoltoworkspace"}
	for _, p := range prefixes {
		if strings.HasPrefix(arg, p) {
			return true
		}
	}
	return false
}

func isDwindleLayoutMsg(arg string) bool {
	return arg == "rotatesplit" || arg == "togglesplit"
}

func dispatcherToLua(dispatcher, arg string) string {
	switch dispatcher {
	case "killactive":
		return "hl.dsp.window.close()"
	case "exit":
		return "hl.dsp.exit()"
	case "togglefloating":
		return "hl.dsp.window.float({ action = \"toggle\" })"
	case "fullscreen":
		if arg == "1" {
			return "hl.dsp.window.fullscreen({ action = \"set\" })"
		} else if arg == "0" {
			return "hl.dsp.window.fullscreen({ action = \"unset\" })"
		}
		return "hl.dsp.window.fullscreen()"
	case "movefocus":
		// In monocle, the spatial direction is meaningless (only one
		// window is visible at a time). Cycle through the window stack
		// instead: up/right = next, down/left = prev. This keeps the
		// same SUPER+Arrow keybinds working across all layouts.
		cycle := "cyclenext"
		if arg == "d" || arg == "l" {
			cycle = "cycleprev"
		}
		return fmt.Sprintf(
			"function() local layout = hl.get_active_workspace().tiled_layout; if layout == \"scrolling\" then hl.dispatch(hl.dsp.layout(%q)) elseif layout == \"monocle\" then hl.dispatch(hl.dsp.layout(%q)) else hl.dispatch(hl.dsp.focus({ direction = %q })) end end",
			"focus "+arg, cycle, arg)
	case "movewindow":
		if arg == "" {
			return "hl.dsp.window.drag()"
		}
		// In monocle there's only one window visible at a time, so
		// directional moves are no-ops. Keep the same keybind working
		// by cycling focus on the user, which is what they'd expect
		// from a "move" gesture in this layout.
		cycle := "cyclenext"
		if arg == "d" || arg == "l" {
			cycle = "cycleprev"
		}
		return fmt.Sprintf(
			"function() local layout = hl.get_active_workspace().tiled_layout; if layout == \"monocle\" then hl.dispatch(hl.dsp.layout(%q)) else hl.dispatch(hl.dsp.window.move({ direction = %q })) end end",
			cycle, arg)
	case "resizewindow":
		if arg == "" {
			return "hl.dsp.window.resize()"
		}
		return fmt.Sprintf("hl.dsp.window.resize({ %s })", arg)
	case "movetoworkspace":
		return fmt.Sprintf("hl.dsp.window.move({ workspace = %q })", arg)
	case "movetoworkspacesilent":
		return fmt.Sprintf("hl.dsp.window.move({ workspace = %q })", arg)
	case "workspace":
		return fmt.Sprintf("hl.dsp.focus({ workspace = %q })", arg)
	case "togglespecialworkspace":
		return fmt.Sprintf("hl.dsp.workspace.toggle_special(%q)", arg)
	case "pin":
		return "hl.dsp.window.pin({ action = \"toggle\" })"
	case "togglegroup":
		return "hl.dsp.group.toggle()"
	case "changegroupactive":
		dir := "next"
		if arg == "b" || arg == "prev" {
			dir = "prev"
		}
		return fmt.Sprintf("hl.dsp.group.%s()", dir)
	case "focuswindow":
		return fmt.Sprintf("hl.dsp.focus({ window = %q })", arg)
	case "closewindow":
		return fmt.Sprintf("hl.dsp.window.close(%q)", arg)
	case "dpms":
		return fmt.Sprintf("hl.dsp.dpms({ action = %q })", arg)
	case "layoutmsg":
		if isScrollingLayoutMsg(arg) {
			return fmt.Sprintf(
				"function() if hl.get_active_workspace().tiled_layout == \"scrolling\" then hl.dispatch(hl.dsp.layout(%q)) end end", arg)
		}
		if isDwindleLayoutMsg(arg) {
			return fmt.Sprintf(
				"function() if hl.get_active_workspace().tiled_layout == \"dwindle\" then hl.dispatch(hl.dsp.layout(%q)) end end", arg)
		}
		return fmt.Sprintf("hl.dsp.layout(%q)", arg)
	case "resizewindowpixel":
		return fmt.Sprintf("hl.dsp.window.resize({ %s })", arg)
	case "movewindowpixel":
		return fmt.Sprintf("hl.dsp.window.move({ %s })", arg)
	case "pseudo":
		return "hl.dsp.window.pseudo()"
	case "centerwindow":
		return "hl.dsp.window.center()"
	default:
		if arg != "" {
			return fmt.Sprintf("hl.dsp.exec_cmd(%q)", dispatcher+" "+arg)
		}
		return fmt.Sprintf("hl.dsp.exec_cmd(%q)", dispatcher)
	}
}

func (g *LuaGenerator) GenerateWindowRulesLua(rules []ipc.WindowRule) string {
	var b strings.Builder
	b.WriteString("-- Generated by axctl LuaGenerator (Window Rules)\n")
	b.WriteString("-- Do not edit manually!\n\n")

	for _, r := range rules {
		if r.Match == "" && r.Name == "" {
			continue
		}

		b.WriteString("hl.window_rule({\n")
		if r.Name != "" {
			b.WriteString(fmt.Sprintf("    name = %q,\n", r.Name))
		}

		match := r.Match
		if match != "" {
			b.WriteString(fmt.Sprintf("    match = { class = %q },\n", match))
		}

		if r.Float != nil && *r.Float {
			b.WriteString("    float = true,\n")
		}
		if r.NoBlur != nil && *r.NoBlur {
			b.WriteString("    no_blur = true,\n")
		}
		if r.NoShadow != nil && *r.NoShadow {
			b.WriteString("    no_shadow = true,\n")
		}
		if r.Rounding != nil {
			b.WriteString(fmt.Sprintf("    rounding = %d,\n", *r.Rounding))
		}
		if r.BorderSize != nil {
			b.WriteString(fmt.Sprintf("    border_size = %d,\n", *r.BorderSize))
		}
		if r.Pin != nil && *r.Pin {
			b.WriteString("    pin = true,\n")
		}
		if r.Fullscreen != nil && *r.Fullscreen {
			b.WriteString("    fullscreen = true,\n")
		}
		if r.IdleInhibit != nil && *r.IdleInhibit {
			b.WriteString("    idle_inhibit = \"always\",\n")
		}
		if r.NoScreenShare != nil && *r.NoScreenShare {
			b.WriteString("    no_screen_share = true,\n")
		}
		if r.Move != nil && *r.Move != "" {
			b.WriteString(fmt.Sprintf("    move = %q,\n", *r.Move))
		}
		if r.Size != nil && *r.Size != "" {
			b.WriteString(fmt.Sprintf("    size = %q,\n", *r.Size))
		}
		if r.Rule != "" {
			b.WriteString(fmt.Sprintf("    -- legacy rule: %q\n", r.Rule))
		}

		b.WriteString("})\n\n")
	}

	return b.String()
}

func (g *LuaGenerator) GenerateLayerRulesLua(rules []ipc.LayerRule) string {
	var b strings.Builder
	b.WriteString("-- Generated by axctl LuaGenerator (Layer Rules)\n")
	b.WriteString("-- Do not edit manually!\n\n")

	for _, r := range rules {
		if r.Namespace == "" {
			continue
		}

		b.WriteString("hl.layer_rule({\n")
		if r.NoAnim != nil && *r.NoAnim {
			b.WriteString("    no_anim = true,\n")
		}
		if r.Blur != nil && *r.Blur {
			b.WriteString("    blur = true,\n")
		}
		if r.BlurPopups != nil && *r.BlurPopups {
			b.WriteString("    blur_popups = true,\n")
		}
		if r.IgnoreAlphaValue != nil {
			b.WriteString(fmt.Sprintf("    ignore_alpha = %.2f,\n", *r.IgnoreAlphaValue))
		} else if r.IgnoreAlpha != nil && *r.IgnoreAlpha {
			b.WriteString("    ignore_alpha = 0,\n")
		}
		if r.IgnoreZeroAlpha != nil && *r.IgnoreZeroAlpha {
			b.WriteString("    ignore_zero = true,\n")
		}
		if r.NoShadow != nil && *r.NoShadow {
			b.WriteString("    no_shadow = true,\n")
		}
		b.WriteString(fmt.Sprintf("    match = { namespace = %q },\n", r.Namespace))
		b.WriteString("})\n\n")
	}

	return b.String()
}

func (g *LuaGenerator) GenerateStartupLua(exec []string, execOnce []string) string {
	if len(exec) == 0 && len(execOnce) == 0 {
		return ""
	}

	var b strings.Builder

	if len(execOnce) > 0 {
		b.WriteString("hl.on(\"hyprland.start\", function()\n")
		for _, cmd := range execOnce {
			if strings.TrimSpace(cmd) == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("    hl.exec_cmd(%q)\n", cmd))
		}
		b.WriteString("end)\n\n")
	}

	for _, cmd := range exec {
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("hl.exec_cmd(%q)\n", cmd))
	}
	return b.String()
}
