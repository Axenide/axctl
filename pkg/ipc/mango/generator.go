package mango

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

func mangoConfigColor(s string) string {
	if s == "" {
		return "0x000000ff"
	}
	c := ipc.MangoColor(s)
	if c == "" {
		return "0x000000ff"
	}
	return c
}

func formatMangoModifiers(mods []string) string {
	if len(mods) == 0 {
		return "none"
	}
	var mapped []string
	for _, m := range mods {
		upper := strings.ToUpper(m)
		switch upper {
		case "SUPER", "MOD4", "WIN":
			mapped = append(mapped, "SUPER")
		case "CTRL", "CONTROL":
			mapped = append(mapped, "CTRL")
		case "ALT":
			mapped = append(mapped, "ALT")
		case "SHIFT":
			mapped = append(mapped, "SHIFT")
		default:
			mapped = append(mapped, upper)
		}
	}
	return strings.Join(mapped, "+")
}

var mangoDispatchers = map[string]string{
	"":                       "spawn_shell",
	"exec":                   "spawn_shell",
	"spawn":                  "spawn_shell",
	"close":                  "killclient",
	"killclient":             "killclient",
	"fullscreen":             "togglefullscreen",
	"togglefullscreen":       "togglefullscreen",
	"maximize":               "togglemaximizescreen",
	"togglemaximizescreen":   "togglemaximizescreen",
	"togglefloating":         "togglefloating",
	"toggle-floating":        "togglefloating",
	"pin":                    "toggleglobal",
	"toggleglobal":           "toggleglobal",
	"toggle-pin":             "toggleglobal",
	"move":                   "move_client",
	"workspace":              "view",
	"movetoworkspace":        "tag",
	"movetoworkspacesilent":  "tagsilent",
	"overview":               "toggleoverview",
	"reload":                 "reload_config",
	"reload_config":          "reload_config",
	"quit":                   "quit",
	"exit":                   "quit",
	"focusdir-left":          "focusdir,left",
	"focusdir-right":         "focusdir,right",
	"focusdir-up":            "focusdir,up",
	"focusdir-down":          "focusdir,down",
	"move-left":              "move_client,left",
	"move-right":             "move_client,right",
	"move-up":                "move_client,up",
	"move-down":              "move_client,down",
	"smartmovewin-left":      "smartmovewin,left",
	"smartmovewin-right":     "smartmovewin,right",
	"smartmovewin-up":        "smartmovewin,up",
	"smartmovewin-down":      "smartmovewin,down",
	"center":                 "centerwin",
	"centerwin":              "centerwin",
	"toggle-scratchpad":      "toggle_scratchpad",
	"toggle_scratchpad":      "toggle_scratchpad",
	"switch-layout":          "switch_layout",
	"switch_layout":          "switch_layout",
	"setlayout":              "setlayout",
	"incnmaster":             "incnmaster",
	"setmfact":               "setmfact",
	"zoom":                   "zoom",
	"focusmon-left":          "focusmon,left",
	"focusmon-right":         "focusmon,right",
	"focusmon-up":            "focusmon,up",
	"focusmon-down":          "focusmon,down",
	"focusmon-next":          "focusmon,next",
	"focusmon-prev":          "focusmon,prev",
	"tagmon-left":            "tagmon,left",
	"tagmon-right":           "tagmon,right",
	"tagmon-up":              "tagmon,up",
	"tagmon-down":            "tagmon,down",
	"viewtoleft":             "viewtoleft",
	"viewtoright":            "viewtoright",
	"focusstack-next":        "focusstack,next",
	"focusstack-prev":        "focusstack,prev",
	"focusstack":             "focusstack,next",
	"groupfocus-next":        "groupfocus,next",
	"groupfocus-prev":        "groupfocus,prev",
	"group-nav-next":         "groupfocus,next",
	"group-nav-prev":         "groupfocus,prev",
	"groupjoin-left":         "groupjoin,left",
	"groupjoin-right":        "groupjoin,right",
	"minimize":               "minimized",
	"minimized":              "minimized",
	"restore-minimized":      "restore_minimized",
	"toggleoverview":         "toggleoverview",
	"togglejump":             "togglejump",
	"togglefakefullscreen":   "togglefakefullscreen",
	"toggleoverlay":          "toggleoverlay",
	"toggle-special":         "toggle_special_tag",
	"toggle_special_tag":     "toggle_special_tag",
	"switch-keyboard-layout": "switch_keyboard_layout",
}

func boolStr(b *bool) string {
	if b == nil {
		return ""
	}
	if *b {
		return "1"
	}
	return "0"
}

func mangoBindFlags(flags string) string {
	flags = strings.ToLower(strings.TrimSpace(flags))
	if flags == "" {
		return ""
	}
	var out strings.Builder
	appendFlag := func(flag byte) {
		if !strings.ContainsRune(out.String(), rune(flag)) {
			out.WriteByte(flag)
		}
	}
	for _, token := range strings.FieldsFunc(flags, func(r rune) bool {
		return r == '-' || r == '_' || r == '+' || r == ',' || r == ' '
	}) {
		switch token {
		case "release":
			appendFlag('r')
		case "lock":
			appendFlag('l')
		case "pass":
			appendFlag('p')
		case "conflict":
			appendFlag('c')
		case "sym":
			appendFlag('s')
		default:
			for _, flag := range token {
				if strings.ContainsRune("rlpcs", flag) {
					appendFlag(byte(flag))
				}
			}
		}
	}
	return out.String()
}

func intStr(i *int) string {
	if i == nil {
		return ""
	}
	return strconv.Itoa(*i)
}

func floatStr(f *float64) string {
	if f == nil {
		return ""
	}
	return strconv.FormatFloat(*f, 'f', -1, 64)
}

func (g *Generator) GenerateAppearance(config ipc.ConfigAppearance) string {
	var b strings.Builder
	b.WriteString("# Generated by axctl\n")
	b.WriteString("# Include this file from your Mango config via `source`.\n\n")

	if config.Gaps != nil {
		if v := intStr(config.Gaps.Inner); v != "" {
			b.WriteString("gap_inner_horizontal = " + v + "\n")
			b.WriteString("gap_inner_vertical = " + v + "\n")
		}
		if v := intStr(config.Gaps.Outer); v != "" {
			b.WriteString("gap_outer_horizontal = " + v + "\n")
			b.WriteString("gap_outer_vertical = " + v + "\n")
		}
	}

	if config.Border != nil {
		if v := intStr(config.Border.Width); v != "" {
			b.WriteString("border_px = " + v + "\n")
		}
		if config.Border.ActiveColor != nil {
			b.WriteString("focus_color = " + mangoConfigColor(*config.Border.ActiveColor) + "\n")
		}
		if config.Border.InactiveColor != nil {
			b.WriteString("border_color = " + mangoConfigColor(*config.Border.InactiveColor) + "\n")
		}
		if v := intStr(config.Border.Rounding); v != "" {
			b.WriteString("border_radius = " + v + "\n")
		}
	}

	if config.Opacity != nil {
		if v := floatStr(config.Opacity.Active); v != "" {
			b.WriteString("focused_opacity = " + v + "\n")
		}
		if v := floatStr(config.Opacity.Inactive); v != "" {
			b.WriteString("unfocused_opacity = " + v + "\n")
		}
	}

	if config.Blur != nil {
		if v := boolStr(config.Blur.Enabled); v != "" {
			b.WriteString("blur = " + v + "\n")
		}
		if v := intStr(config.Blur.Size); v != "" {
			b.WriteString("blur_params_radius = " + v + "\n")
		}
		if v := intStr(config.Blur.Passes); v != "" {
			b.WriteString("blur_params_num_passes = " + v + "\n")
		}
	}

	if config.Shadow != nil {
		if v := boolStr(config.Shadow.Enabled); v != "" {
			b.WriteString("shadows = " + v + "\n")
		}
		if v := intStr(config.Shadow.Size); v != "" {
			b.WriteString("shadows_size = " + v + "\n")
		}
		if config.Shadow.Color != nil {
			b.WriteString("shadows_color = " + mangoConfigColor(*config.Shadow.Color) + "\n")
		}
	}

	if config.Animations != nil && config.Animations.Enabled != nil {
		b.WriteString(fmt.Sprintf("animations = %s\n", boolStr(config.Animations.Enabled)))
	}

	if config.Layout != nil && *config.Layout != "" {
		b.WriteString("tag_rule=id:*,layout_name:" + *config.Layout + "\n")
	}
	return b.String()
}

func (g *Generator) GenerateKeybinds(config ipc.ConfigKeybinds) string {
	var b strings.Builder
	b.WriteString("# Generated by axctl (keybinds)\n")
	b.WriteString("# Mango bind syntax: bind=MOD1+MOD2,KEY,COMMAND,ARGS\n\n")
	var skipped []string

	addBind := func(kb ipc.Keybind, comment string) {
		if !kb.Enabled || kb.Key == "" {
			return
		}
		if !mangoSupportsKey(kb.Key) {
			skipped = append(skipped, fmt.Sprintf("%s — key %q is not supported in Mango", comment, kb.Key))
			return
		}
		mods := formatMangoModifiers(kb.Modifiers)
		dispatcher, argument, ok := mangoMapKeybind(kb)
		if !ok {
			skipped = append(skipped, fmt.Sprintf("%s — dispatcher %q is not supported in Mango", comment, kb.Dispatcher))
			return
		}

		key := strings.ToUpper(kb.Key)
		flags := mangoBindFlags(kb.Flags)
		if ipc.ModifierSelfGroup(kb) != "" && !strings.ContainsRune(flags, 'r') {
			flags += "r"
		}

		line := fmt.Sprintf("bind%s = %s,%s,%s", flags, mods, key, dispatcher)
		if strings.TrimSpace(argument) != "" {
			line += "," + argument
		}
		if comment != "" {
			line += " # " + comment
		}
		b.WriteString(line + "\n")
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
	if len(skipped) > 0 {
		b.WriteString("\n# Skipped binds without a Mango equivalent:\n")
		for _, item := range skipped {
			b.WriteString("# " + item + "\n")
		}
	}
	return b.String()
}

func mangoSupportsKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" || strings.ContainsAny(key, ",\r\n") {
		return false
	}
	lower := strings.ToLower(key)
	return lower != "mouse_down" && lower != "mouse_up" && !strings.HasPrefix(lower, "switch:")
}

func mangoMapKeybind(kb ipc.Keybind) (string, string, bool) {
	dispatcher := strings.ToLower(strings.TrimSpace(kb.Dispatcher))
	argument := strings.TrimSpace(kb.Argument)
	switch dispatcher {
	case "killactive":
		return "killclient", "", true
	case "movefocus":
		direction, ok := mangoKeybindDirection(argument)
		return "focusdir", direction, ok
	case "movewindow":
		direction, ok := mangoKeybindDirection(argument)
		return "exchange_client", direction, ok
	case "workspace":
		return mangoMapWorkspaceArgument(argument)
	case "resizeactive":
		parts := strings.Fields(strings.NewReplacer(",", " ", ";", " ").Replace(argument))
		if len(parts) != 2 {
			return "", "", false
		}
		x, xErr := strconv.Atoi(parts[0])
		y, yErr := strconv.Atoi(parts[1])
		if xErr != nil || yErr != nil {
			return "", "", false
		}
		return "resizewin", fmt.Sprintf("%+d,%+d", x, y), true
	case "togglespecialworkspace":
		if argument == "" || strings.EqualFold(argument, "special") {
			return "toggle_special_tag", "", true
		}
		return "", "", false
	case "movetoworkspace":
		if strings.EqualFold(argument, "special") {
			return "toggle_scratchpad", "", true
		}
	case "layoutmsg":
		return mangoMapLayoutMessage(argument)
	}
	mapped, ok := mangoMapDispatcher(kb.Dispatcher)
	return mapped, argument, ok
}

func mangoMapWorkspaceArgument(argument string) (string, string, bool) {
	switch strings.ToLower(strings.TrimSpace(argument)) {
	case "-1", "r-1":
		return "viewtoleft", "", true
	case "+1", "r+1":
		return "viewtoright", "", true
	case "e-1":
		return "viewtoleft_have_client", "", true
	case "e+1":
		return "viewtoright_have_client", "", true
	default:
		return "view", strings.TrimSpace(argument), true
	}
}

func mangoKeybindDirection(direction string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "l", "left":
		return "left", true
	case "r", "right":
		return "right", true
	case "u", "up":
		return "up", true
	case "d", "down":
		return "down", true
	default:
		return "", false
	}
}

func mangoMapLayoutMessage(message string) (string, string, bool) {
	parts := strings.Fields(message)
	if len(parts) == 0 {
		return "", "", false
	}
	switch strings.ToLower(parts[0]) {
	case "center":
		return "centerwin", "", len(parts) == 1
	case "promote":
		return "zoom", "", len(parts) == 1
	case "swapcol":
		if len(parts) != 2 {
			return "", "", false
		}
		direction, ok := mangoKeybindDirection(parts[1])
		return "exchange_client", direction, ok
	case "movecoltoworkspace":
		if len(parts) != 2 {
			return "", "", false
		}
		if _, err := strconv.Atoi(parts[1]); err != nil {
			return "", "", false
		}
		return "tag", parts[1], true
	default:
		return "", "", false
	}
}

func mangoMapDispatcher(dispatcher string) (string, bool) {
	dispatcher = strings.ToLower(strings.TrimSpace(dispatcher))
	if mapped, ok := mangoDispatchers[dispatcher]; ok {
		return mapped, true
	}
	switch dispatcher {
	case "focusdir", "focusstack", "groupfocus", "groupjoin", "groupleave",
		"killclient", "smartkillclient", "centerwin", "focuslast", "setoption",
		"switch_keyboard_layout", "setlayout", "switch_layout", "togglefloating",
		"togglefullscreen", "togglefakefullscreen", "toggleoverlay", "minimized",
		"restore_minimized", "toggle_scratchpad", "toggle_special_tag", "toggleoverview",
		"togglejump", "toggleglobal", "togglemaximizescreen", "view", "tag",
		"tagsilent", "viewcrossmon", "tagcrossmon", "toggletag", "toggleview",
		"spawn", "spawn_shell", "quit", "resizewin", "movewin", "smartmovewin",
		"move_client", "exchange_client", "set_proportion", "focusmon", "tagmon",
		"sleep_monitor", "wakeup_monitor", "reload_config", "viewtoleft_have_client",
		"viewtoright_have_client":
		return dispatcher, true
	default:
		return "", false
	}
}

func (g *Generator) GenerateWindowRules(rules []ipc.WindowRule) string {
	var b strings.Builder
	b.WriteString("# Generated by axctl (window rules)\n\n")

	for _, r := range rules {
		var params []string
		unsupported := []string{}
		if r.Match != "" {
			property, value, ok := mangoRuleMatch(r.Match)
			if !ok || strings.ContainsAny(value, ",\r\n") {
				unsupported = append(unsupported, "match: "+r.Match)
			} else {
				params = append(params, property+":"+value)
			}
		}
		if r.Float != nil {
			params = append(params, "is_floating:"+boolStr(r.Float))
		}
		if r.NoBlur != nil {
			params = append(params, "no_blur:"+boolStr(r.NoBlur))
		}
		if r.NoShadow != nil {
			params = append(params, "no_shadow:"+boolStr(r.NoShadow))
		}
		if r.Rounding != nil {
			if *r.Rounding == 0 {
				params = append(params, "no_radius:1")
			} else {
				unsupported = append(unsupported, "per-window rounding")
			}
		}
		if r.BorderSize != nil {
			if *r.BorderSize == 0 {
				params = append(params, "no_border:1")
			} else {
				unsupported = append(unsupported, "per-window border size")
			}
		}
		if r.Pin != nil {
			params = append(params, "is_global:"+boolStr(r.Pin))
		}
		if r.Fullscreen != nil {
			params = append(params, "is_fullscreen:"+boolStr(r.Fullscreen))
		}
		if r.IdleInhibit != nil {
			params = append(params, "idle_inhibit_when_focus:"+boolStr(r.IdleInhibit))
		}
		if r.NoScreenShare != nil {
			params = append(params, "shield_when_capture:"+boolStr(r.NoScreenShare))
		}
		if r.Move != nil {
			move, err := mangoRuleMove(*r.Move)
			if err != nil {
				unsupported = append(unsupported, "move: "+*r.Move)
			} else {
				params = append(params, move...)
			}
		}
		if r.Size != nil {
			size, err := mangoRuleSize(*r.Size)
			if err != nil {
				unsupported = append(unsupported, "size: "+*r.Size)
			} else {
				params = append(params, size...)
			}
		}
		if r.Rule != "" {
			legacy, ok := mangoLegacyWindowRule(r.Rule)
			if ok {
				params = append(params, legacy...)
			} else {
				unsupported = append(unsupported, "legacy rule: "+r.Rule)
			}
		}
		if r.Action != "" {
			unsupported = append(unsupported, "action: "+r.Action)
		}
		if r.Name != "" {
			unsupported = append(unsupported, "named rule: "+r.Name)
		}
		if len(params) > 0 {
			b.WriteString("window_rule=" + strings.Join(params, ",") + "\n")
		}
		if len(unsupported) > 0 {
			b.WriteString("# Unsupported Mango window rule properties: " + strings.Join(unsupported, ", ") + "\n")
		}
	}
	return b.String()
}

func mangoLegacyWindowRule(rule string) ([]string, bool) {
	parts := strings.Fields(strings.TrimSpace(rule))
	if len(parts) == 0 {
		return nil, false
	}
	if strings.Contains(rule, ":") && !strings.ContainsAny(rule, "\r\n") {
		return []string{rule}, true
	}
	if len(parts) == 2 {
		switch strings.ToLower(parts[0]) {
		case "opacity":
			return []string{"focused_opacity:" + parts[1], "unfocused_opacity:" + parts[1]}, true
		case "float", "floating":
			return []string{"is_floating:" + parts[1]}, true
		case "fullscreen":
			return []string{"is_fullscreen:" + parts[1]}, true
		}
	}
	return nil, false
}

func mangoRuleMatch(match string) (string, string, bool) {
	property, value, ok := strings.Cut(match, ":")
	if !ok {
		return "app_id", strings.TrimSpace(match), true
	}
	property = strings.ToLower(strings.TrimSpace(property))
	value = strings.TrimSpace(value)
	if property == "class" || property == "appid" {
		property = "app_id"
	}
	if property != "title" && property != "app_id" {
		return "", "", false
	}
	return property, value, true
}

func mangoRuleMove(move string) ([]string, error) {
	move = strings.TrimSpace(move)
	if move == "" {
		return nil, nil
	}
	if strings.EqualFold(move, "center") {
		return []string{"offset_x:0", "offset_y:0"}, nil
	}
	return nil, fmt.Errorf("absolute pixel positioning has no Mango window-rule equivalent")
}

func mangoRuleSize(size string) ([]string, error) {
	parts := strings.SplitN(strings.TrimSpace(size), "x", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("expected widthxheight")
	}
	width, err := mangoRuleDimension(parts[0])
	if err != nil {
		return nil, err
	}
	height, err := mangoRuleDimension(parts[1])
	if err != nil {
		return nil, err
	}
	return []string{"width:" + width, "height:" + height}, nil
}

func mangoRuleDimension(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasSuffix(raw, "%") {
		percent, err := strconv.ParseFloat(strings.TrimSuffix(raw, "%"), 64)
		if err != nil || percent <= 0 || percent > 100 {
			return "", fmt.Errorf("invalid percentage dimension %q", raw)
		}
		return strconv.FormatFloat(percent/100, 'f', -1, 64), nil
	}
	dimension, err := strconv.Atoi(raw)
	if err != nil || dimension <= 0 {
		return "", fmt.Errorf("invalid pixel dimension %q", raw)
	}
	return strconv.Itoa(dimension), nil
}

func (g *Generator) GenerateLayerRules(rules []ipc.LayerRule) string {
	var b strings.Builder
	b.WriteString("# Generated by axctl (layer rules)\n\n")
	for _, rule := range rules {
		if rule.Namespace == "" {
			continue
		}
		if strings.ContainsAny(rule.Namespace, ",\r\n") {
			b.WriteString("# Unsupported Mango layer rule namespace: " + rule.Namespace + "\n")
			continue
		}
		var params []string
		params = append(params, "layer_name:"+rule.Namespace)
		var unsupported []string
		if rule.NoAnim != nil {
			params = append(params, "no_animation:"+boolStr(rule.NoAnim))
		}
		if rule.Blur != nil {
			noBlur := !*rule.Blur
			params = append(params, "no_blur:"+boolStr(&noBlur))
		}
		if rule.NoShadow != nil {
			params = append(params, "no_shadow:"+boolStr(rule.NoShadow))
		}
		if rule.BlurPopups != nil || rule.IgnoreAlpha != nil || rule.IgnoreZeroAlpha != nil || rule.IgnoreAlphaValue != nil {
			unsupported = append(unsupported, "blur popups/alpha controls")
		}
		if rule.PlaceWithinBackdrop != nil {
			unsupported = append(unsupported, "place within overview backdrop")
		}
		b.WriteString("layer_rule=" + strings.Join(params, ",") + "\n")
		if len(unsupported) > 0 {
			b.WriteString("# Unsupported Mango layer rule properties: " + strings.Join(unsupported, ", ") + "\n")
		}
	}
	return b.String()
}

func (g *Generator) GenerateStartup(exec []string, execOnce []string) string {
	if len(exec) == 0 && len(execOnce) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Generated by axctl (startup)\n\n")
	for _, cmd := range execOnce {
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		b.WriteString("exec_once = " + cmd + "\n")
	}
	for _, cmd := range exec {
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		b.WriteString("exec = " + cmd + "\n")
	}
	return b.String()
}
