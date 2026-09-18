package niri

import (
	"fmt"
	"strconv"
	"strings"

	"axctl/pkg/ipc"
)

type Generator struct{}

func NewGenerator() *Generator {
	return &Generator{}
}

func niriKdlColor(hexStr string) string {
	if hexStr == "" {
		return ""
	}
	hexStr = strings.TrimPrefix(hexStr, "#")
	switch len(hexStr) {
	case 6:
		return "#" + hexStr + "ff"
	case 8:
		return "#" + hexStr
	case 3:
		r := string([]byte{hexStr[0], hexStr[0], hexStr[1], hexStr[1], hexStr[2], hexStr[2], 'f', 'f'})
		return "#" + r
	case 4:
		r := string([]byte{hexStr[0], hexStr[0], hexStr[1], hexStr[1], hexStr[2], hexStr[2], hexStr[3], hexStr[3]})
		return "#" + r
	}
	return "#" + hexStr
}

func niriFirstColor(s string) string {
	parts := strings.Fields(s)
	for _, p := range parts {
		if strings.HasSuffix(p, "deg") {
			continue
		}
		return niriParseColor(p)
	}
	return ""
}

// niriParseColor converts a single color token into Niri's #rrggbb[aa] form.
// It accepts Hyprland-style rgb()/rgba() wrappers (the format ambxst stores
// in the TOML) and bare hex strings. Niri rejects "rgb(87abf8)" as invalid
// hex, so the wrapper has to be unwrapped here.
func niriParseColor(token string) string {
	if token == "" {
		return ""
	}
	if strings.HasPrefix(token, "rgb(") || strings.HasPrefix(token, "rgba(") {
		hex := strings.TrimPrefix(strings.TrimPrefix(token, "rgba("), "rgb(")
		hex = strings.TrimSuffix(hex, ")")
		return niriKdlColor(hex)
	}
	return niriKdlColor(token)
}

func formatNiriModifiers(mods []string) string {
	if len(mods) == 0 {
		return ""
	}
	var mapped []string
	for _, m := range mods {
		switch strings.ToUpper(m) {
		case "SUPER":
			mapped = append(mapped, "Mod")
		case "CTRL", "CONTROL":
			mapped = append(mapped, "Ctrl")
		case "ALT":
			mapped = append(mapped, "Alt")
		case "SHIFT":
			mapped = append(mapped, "Shift")
		default:
			mapped = append(mapped, m)
		}
	}
	return strings.Join(mapped, "+")
}

// niriMapKey translates a Hyprland-style key into the closest Niri binding.
// Niri's KDL identifier rules are stricter than Hyprland's, so a Hyprland
// key like "mouse:272" or "switch:Lid Switch" can't be passed through as-is.
// Returns the translated key plus a flag indicating whether the key is
// representable in Niri at all — empty + false means "skip this bind".
func niriMapKey(key string) (string, bool) {
	switch key {
	case "mouse:272":
		return "MouseLeft", true
	case "mouse:273":
		return "MouseRight", true
	case "mouse:274":
		return "MouseMiddle", true
	case "mouse:275":
		return "MouseForward", true
	case "mouse:276":
		return "MouseBack", true
	}
	// Lid switch events and other hardware-specific Hyprland keys have no
	// Niri equivalent; emitting them produces invalid KDL ("switch:Lid
	// Switch" can't be parsed as a single identifier).
	if strings.HasPrefix(key, "switch:") {
		return "", false
	}
	// mouse_down / mouse_up are Hyprland wheel bindings; Niri has no
	// wheel keybind facility.
	if key == "mouse_down" || key == "mouse_up" {
		return "", false
	}
	return key, true
}

func kdlQuote(s string) string {
	if s == "" {
		return "\"\""
	}
	if !strings.ContainsAny(s, " \t\"\\{}#\n") {
		return "\"" + s + "\""
	}
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`).Replace(s)
	return "\"" + escaped + "\""
}

// niriModifierSelfBind reports whether a bind's trigger key is a modifier
// key included in its own modifier set (e.g. Mod+Super_L). Niri fires binds
// on key press only — there is no release-based trigger — so such a bind
// would fire on every press of the modifier and interfere with
// modifier+key combos. The second return is the reason for the skip.
func niriModifierSelfBind(key string, mods []string) (string, bool) {
	has := func(names ...string) bool {
		for _, m := range mods {
			for _, n := range names {
				if strings.EqualFold(m, n) {
					return true
				}
			}
		}
		return false
	}
	switch key {
	case "Super_L", "Super_R":
		if has("SUPER", "MOD") {
			return "niri fires binds on press, so binding the modifier key itself would trigger on every Super press and interfere with Super+key combos; niri has no press-Super-alone (release-based) bind support", true
		}
	case "Alt_L", "Alt_R":
		if has("ALT") {
			return "niri fires binds on press, so binding the modifier key itself would trigger on every Alt press; niri has no release-based bind support", true
		}
	case "Control_L", "Control_R":
		if has("CTRL", "CONTROL") {
			return "niri fires binds on press, so binding the modifier key itself would trigger on every Ctrl press; niri has no release-based bind support", true
		}
	case "Shift_L", "Shift_R":
		if has("SHIFT") {
			return "niri fires binds on press, so binding the modifier key itself would trigger on every Shift press; niri has no release-based bind support", true
		}
	}
	return "", false
}

var niriDispatchers = map[string]string{
	"exec":                         "spawn",
	"spawn":                        "spawn",
	"close":                        "close-window",
	"close-window":                 "close-window",
	"fullscreen":                   "fullscreen-window",
	"fullscreen-window":            "fullscreen-window",
	"maximize":                     "maximize-window",
	"maximize-window":              "maximize-window",
	"unmaximize":                   "unmaximize-window",
	"unmaximize-window":            "unmaximize-window",
	"toggle-floating":              "toggle-window-floating",
	"togglefloating":               "toggle-window-floating",
	"toggle-window-floating":       "toggle-window-floating",
	"focus-column-left":            "focus-column-left",
	"focus-column-right":           "focus-column-right",
	"focus-window-up":              "focus-window-up",
	"focus-window-down":            "focus-window-down",
	"focus-workspace-up":           "focus-workspace-up",
	"focus-workspace-down":         "focus-workspace-down",
	"focus-monitor-left":           "focus-monitor-left",
	"focus-monitor-right":          "focus-monitor-right",
	"focus-monitor-up":             "focus-monitor-up",
	"focus-monitor-down":           "focus-monitor-down",
	"move-column-left":             "move-column-left",
	"move-column-right":            "move-column-right",
	"move-window-up":               "move-window-up",
	"move-window-down":             "move-window-down",
	"quit":                         "quit",
	"do-screen-transition":         "do-screen-transition",
	"switch-preset-column-width":   "switch-preset-column-width",
	"switch-preset-window-height":  "switch-preset-window-height",
	"reset-window-height":          "reset-window-height",
	"center-column":                "center-column",
	"center-window":                "center-window",
	"consume-window-into-column":   "consume-window-into-column",
	"expel-window-from-column":     "expel-window-from-column",
	"toggle-column-tabbed-display": "toggle-column-tabbed-display",
}

// niriMapDispatcher translates a Hyprland dispatcher + argument into a full
// niri action statement (ready to emit inside a bind node). The second
// return value reports whether the translation is possible — false means
// the dispatcher is Hyprland-specific and the caller emits a "skipped"
// comment so the user can wire it up by hand.
func niriMapDispatcher(d, arg string) (string, bool) {
	if d == "" {
		return "", false
	}
	switch d {
	case "killactive":
		return "close-window", true
	case "exit":
		return "quit", true
	case "fullscreen":
		// Hyprland: 0 = fullscreen toggle, 1 = maximize toggle.
		if arg == "1" {
			return "maximize-window", true
		}
		return "fullscreen-window", true
	case "movefocus":
		switch arg {
		case "l":
			return "focus-column-left", true
		case "r":
			return "focus-column-right", true
		case "u":
			return "focus-window-up", true
		case "d":
			return "focus-window-down", true
		}
		return "", false
	case "movewindow":
		switch arg {
		case "l":
			return "move-column-left", true
		case "r":
			return "move-column-right", true
		case "u":
			return "move-window-up", true
		case "d":
			return "move-window-down", true
		}
		return "", false
	case "workspace":
		ref, dir, ok := niriWorkspaceTarget(arg)
		if !ok {
			return "", false
		}
		if dir > 0 {
			return "focus-workspace-down", true
		}
		if dir < 0 {
			return "focus-workspace-up", true
		}
		return formatNiriAction("focus-workspace", ref), true
	case "movetoworkspace":
		ref, dir, ok := niriWorkspaceTarget(arg)
		if !ok {
			return "", false
		}
		switch {
		case dir > 0:
			return "move-window-to-workspace-down", true
		case dir < 0:
			return "move-window-to-workspace-up", true
		}
		return formatNiriAction("move-window-to-workspace", ref), true
	case "movetoworkspacesilent":
		ref, dir, ok := niriWorkspaceTarget(arg)
		if !ok {
			return "", false
		}
		switch {
		case dir > 0:
			return "move-window-to-workspace-down focus=false", true
		case dir < 0:
			return "move-window-to-workspace-up focus=false", true
		}
		return formatNiriAction("move-window-to-workspace", ref) + " focus=false", true
	case "resizeactive":
		// "dx dy" pixel deltas. niri resizes width and height through
		// separate actions, so pick the non-zero axis. "+N"/"-N" parse as
		// SizeChange::AdjustFixed (a resize delta), "N" would be SetFixed.
		// SizeChange args are knuffel string scalars, so quote them — a bare
		// "+50" is an invalid KDL integer literal.
		fields := strings.Fields(arg)
		dx, dy := 0, 0
		if len(fields) > 0 {
			dx, _ = strconv.Atoi(fields[0])
		}
		if len(fields) > 1 {
			dy, _ = strconv.Atoi(fields[1])
		}
		switch {
		case dx != 0:
			return "set-column-width " + kdlQuote(fmt.Sprintf("%+d", dx)), true
		case dy != 0:
			return "set-window-height " + kdlQuote(fmt.Sprintf("%+d", dy)), true
		}
		return "", false
	case "layoutmsg":
		fields := strings.Fields(arg)
		if len(fields) == 0 {
			return "", false
		}
		switch fields[0] {
		case "promote", "togglefit":
			return "maximize-column", true
		case "colresize":
			if len(fields) > 1 && isNiriSizeChange(fields[1]) {
				return "set-column-width " + kdlQuote(fields[1]), true
			}
		}
		return "", false
	case "togglespecialworkspace":
		return "", false
	}
	if v, ok := niriDispatchers[d]; ok {
		return formatNiriAction(v, arg), true
	}
	return "", false
}

// niriWorkspaceTarget normalizes a Hyprland workspace argument into a niri
// reference. dir is +1/-1 for relative moves ("+1", "-1", "e+1", "e-1") and
// 0 for an absolute reference (index or name). Hyprland-only targets
// ("empty", "previous", "mousetoward", "reset") are rejected.
func niriWorkspaceTarget(arg string) (ref string, dir int, ok bool) {
	rel := strings.TrimPrefix(arg, "e")
	if len(rel) > 1 && (rel[0] == '+' || rel[0] == '-') && isNumeric(rel[1:]) {
		if rel[0] == '+' {
			return "", 1, true
		}
		return "", -1, true
	}
	switch arg {
	case "":
		return "", 0, false
	case "previous", "empty", "reset", "all":
		return "", 0, false
	}
	if strings.HasPrefix(arg, "mousetoward") || !isNumeric(arg) && strings.ContainsAny(arg, " \t") {
		return "", 0, false
	}
	return arg, 0, true
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isNiriSizeChange reports whether a token is a valid niri SizeChange
// argument: an integer (fixed px, "+N"/"-N" adjust) or "N%"/"+N%" proportion.
func isNiriSizeChange(s string) bool {
	body := strings.TrimSuffix(s, "%")
	if !isNumeric(strings.TrimPrefix(body, "+")) && !isNumeric(strings.TrimPrefix(body, "-")) {
		return false
	}
	if strings.HasSuffix(s, "%") {
		return true
	}
	_, err := strconv.Atoi(s)
	return err == nil
}

// formatNiriAction appends a formatted argument to an action name: integers
// unquoted (niri workspace references), everything else KDL-quoted.
func formatNiriAction(name, arg string) string {
	if arg == "" {
		return name
	}
	if i, err := strconv.Atoi(arg); err == nil {
		return fmt.Sprintf("%s %d", name, i)
	}
	return name + " " + kdlQuote(arg)
}

func (g *Generator) GenerateAppearance(config ipc.ConfigAppearance) string {
	var b strings.Builder
	b.WriteString("// Generated by axctl\n")
	b.WriteString("// Include this file from your niri config with: include \"<this-file>\"\n\n")

	hasLayout := false
	if config.Gaps != nil && config.Gaps.Inner != nil {
		if !hasLayout {
			b.WriteString("layout {\n")
			hasLayout = true
		}
		b.WriteString(fmt.Sprintf("    gaps %d\n", *config.Gaps.Inner))
	}
	if config.Border != nil {
		if !hasLayout {
			b.WriteString("layout {\n")
			hasLayout = true
		}
		// The border replaces the focus ring (they would double-draw around
		// the focused window), and `on` is required because niri ships the
		// border disabled by default.
		b.WriteString("    focus-ring {\n        off\n    }\n")
		b.WriteString("    border {\n        on\n")
		if config.Border.Width != nil {
			b.WriteString(fmt.Sprintf("        width %d\n", *config.Border.Width))
		}
		if config.Border.ActiveColor != nil {
			color := niriFirstColor(*config.Border.ActiveColor)
			if color != "" {
				b.WriteString(fmt.Sprintf("        active-color %s\n", kdlQuote(color)))
			}
		}
		if config.Border.InactiveColor != nil {
			color := niriFirstColor(*config.Border.InactiveColor)
			if color != "" {
				b.WriteString(fmt.Sprintf("        inactive-color %s\n", kdlQuote(color)))
			}
		}
		b.WriteString("    }\n")
	}
	if hasLayout {
		b.WriteString("}\n\n")
	}

	// Rounding is applied to all windows via a match-less window-rule.
	if config.Border != nil && config.Border.Rounding != nil {
		b.WriteString(fmt.Sprintf("\nwindow-rule {\n    geometry-corner-radius %d\n    clip-to-geometry true\n}\n", *config.Border.Rounding))
	}

	unsupported := []string{}
	if config.Gaps != nil && config.Gaps.Outer != nil {
		unsupported = append(unsupported, "outer gaps (use inner gaps in niri)")
	}
	if config.Opacity != nil {
		unsupported = append(unsupported, "opacity (niri uses per-app window-rule opacity)")
	}
	if config.Blur != nil {
		unsupported = append(unsupported, "blur (configure via blur {} block at top level in niri)")
	}
	if config.Shadow != nil {
		unsupported = append(unsupported, "shadow (configure per-app via window-rule shadow {})")
	}
	if len(unsupported) > 0 {
		b.WriteString(fmt.Sprintf("// Not supported in niri static config: %s\n\n", strings.Join(unsupported, "; ")))
	}

	if config.Animations != nil && config.Animations.Enabled != nil {
		if *config.Animations.Enabled {
			// Niri's animations are on by default; an empty block is the
			// explicit "enabled" form. The earlier "enabled true" body
			// broke `niri validate` because animations doesn't have an
			// `enabled` key.
		} else {
			b.WriteString("animations {\n    off\n}\n")
		}
	}
	if config.Layout != nil && *config.Layout != "" {
		b.WriteString(fmt.Sprintf("// layout switch to %q is not supported by niri (it uses continuous scrolling)\n", *config.Layout))
	}
	return b.String()
}

func (g *Generator) GenerateKeybinds(config ipc.ConfigKeybinds) string {
	var b strings.Builder
	b.WriteString("// Generated by axctl (keybinds)\n\n")
	b.WriteString("binds {\n")

	var skipped []string

	addBind := func(kb ipc.Keybind, comment string) {
		if !kb.Enabled || kb.Key == "" {
			return
		}
		key, ok := niriMapKey(kb.Key)
		if !ok {
			skipped = append(skipped, fmt.Sprintf("%s — %s (key %q has no niri equivalent)", comment, kb.Dispatcher, kb.Key))
			return
		}
		if reason, bad := niriModifierSelfBind(key, kb.Modifiers); bad {
			skipped = append(skipped, fmt.Sprintf("%s — %s (key %q: %s)", comment, kb.Dispatcher, kb.Key, reason))
			return
		}
		arg := kb.Argument

		isSpawn := kb.Dispatcher == "" || kb.Dispatcher == "exec" || kb.Dispatcher == "spawn"
		var action string
		if !isSpawn {
			var ok bool
			action, ok = niriMapDispatcher(kb.Dispatcher, arg)
			if !ok {
				skipped = append(skipped, fmt.Sprintf("%s — %s (dispatcher not supported in niri)", comment, kb.Dispatcher))
				return
			}
		}

		combo := formatNiriModifiers(kb.Modifiers)
		if combo != "" {
			combo += "+"
		}
		combo += key

		if kb.Flags != "" {
			b.WriteString(fmt.Sprintf("    %s repeat=false {\n", combo))
		} else {
			b.WriteString(fmt.Sprintf("    %s {\n", combo))
		}

		if isSpawn {
			parts := tokenizeShell(arg)
			if len(parts) == 0 {
				parts = []string{arg}
			}
			b.WriteString("        spawn")
			for _, p := range parts {
				b.WriteString(" " + kdlQuote(p))
			}
			b.WriteString("\n")
		} else {
			b.WriteString("        " + action + "\n")
		}

		b.WriteString("    }")
		if comment != "" {
			b.WriteString(" // " + comment)
		}
		b.WriteString("\n")
	}

	if config.Ambxst != nil {
		if config.Ambxst.System != nil {
			for name, kb := range config.Ambxst.System {
				addBind(kb, "Ambxst System: "+name)
			}
		}
		if config.Ambxst.Binds != nil {
			for name, kb := range config.Ambxst.Binds {
				addBind(kb, "Ambxst: "+name)
			}
		}
	}
	if config.Custom != nil {
		for i, kb := range config.Custom {
			addBind(kb, fmt.Sprintf("Custom Bind %d", i))
		}
	}

	b.WriteString("}\n")

	if len(skipped) > 0 {
		b.WriteString("\n// Skipped binds (no niri equivalent — wire these manually if needed):\n")
		for _, s := range skipped {
			b.WriteString("//   " + s + "\n")
		}
	}
	return b.String()
}

func tokenizeShell(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	var cur strings.Builder
	inQuote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '"' || c == '\'':
			inQuote = c
		case c == ' ' || c == '\t':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func (g *Generator) GenerateWindowRules(rules []ipc.WindowRule) string {
	var b strings.Builder
	b.WriteString("// Generated by axctl (window rules)\n\n")

	for _, r := range rules {
		legacy := r.Match != "" && r.Rule != ""
		block := r.Float != nil || r.NoBlur != nil || r.NoShadow != nil ||
			r.Rounding != nil || r.BorderSize != nil || r.Pin != nil ||
			r.Fullscreen != nil || r.IdleInhibit != nil || r.NoScreenShare != nil ||
			r.Move != nil || r.Size != nil

		if legacy && !block {
			matchExpr := convertNiriMatch(r.Match)
			b.WriteString(fmt.Sprintf("window-rule {\n    match %s\n    %s\n}\n\n", matchExpr, r.Rule))
			continue
		}

		if r.Match == "" {
			continue
		}
		matchExpr := convertNiriMatch(r.Match)
		b.WriteString(fmt.Sprintf("window-rule {\n    match %s\n", matchExpr))

		if r.Float != nil {
			b.WriteString(fmt.Sprintf("    open-floating %s\n", boolKdl(*r.Float)))
		}
		if r.Fullscreen != nil {
			b.WriteString(fmt.Sprintf("    open-fullscreen %s\n", boolKdl(*r.Fullscreen)))
		}
		if r.Rounding != nil {
			b.WriteString(fmt.Sprintf("    geometry-corner-radius %d\n", *r.Rounding))
		}
		if r.BorderSize != nil {
			b.WriteString(fmt.Sprintf("    border {\n        width %d\n    }\n", *r.BorderSize))
		}
		if r.NoBlur != nil && *r.NoBlur {
			b.WriteString("    background-effect {\n        off\n    }\n")
		}
		if r.NoShadow != nil && *r.NoShadow {
			b.WriteString("    shadow {\n        off\n    }\n")
		}
		if r.Pin != nil && *r.Pin {
			b.WriteString("    // niri does not have a pin/sticky concept; map via open-on-workspace as needed\n")
		}
		if r.IdleInhibit != nil && *r.IdleInhibit {
			b.WriteString("    block-out-from \"screencast\"\n")
		}
		if r.NoScreenShare != nil && *r.NoScreenShare {
			b.WriteString("    block-out-from \"screencast\"\n")
		}
		if r.Move != nil && *r.Move != "" {
			b.WriteString(fmt.Sprintf("    // move %s (use per-output config in niri)\n", *r.Move))
		}
		if r.Size != nil && *r.Size != "" {
			b.WriteString(fmt.Sprintf("    // size %s (use default-window-width/height per-column)\n", *r.Size))
		}
		if r.Rule != "" {
			b.WriteString("    " + r.Rule + "\n")
		}
		b.WriteString("}\n\n")
	}
	return b.String()
}

func boolKdl(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func convertNiriMatch(m string) string {
	if m == "" {
		return ""
	}
	parts := strings.SplitN(m, ":", 2)
	if len(parts) < 2 {
		return kdlQuote(m)
	}
	prop, val := parts[0], strings.TrimSuffix(strings.TrimPrefix(strings.Trim(parts[1], "^$"), "("), ")")
	if prop == "class" {
		prop = "app-id"
	}
	if prop == "title" {
		return fmt.Sprintf("app-id=%s", kdlQuote("")) + " // title match below\n    title=" + kdlQuote(val)
	}
	return fmt.Sprintf("%s=%s", prop, kdlQuote(val))
}

func (g *Generator) GenerateLayerRules(rules []ipc.LayerRule) string {
	return "// Layer rules not supported in niri\n"
}

func (g *Generator) GenerateStartup(exec []string, execOnce []string) string {
	var b strings.Builder
	if len(exec) == 0 && len(execOnce) == 0 {
		return ""
	}
	b.WriteString("// Generated by axctl (startup)\n")
	b.WriteString("// niri has no exec-recurrent; exec-once maps to spawn-at-startup.\n\n")
	for _, cmd := range execOnce {
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("spawn-at-startup %s\n", kdlQuote(cmd)))
	}
	for _, cmd := range exec {
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("// niri does not re-run exec on reload; ignoring: %s\n", cmd))
	}
	return b.String()
}
