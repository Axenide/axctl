package hyprland

import (
	"fmt"
	"strings"

	"axctl/pkg/ipc"
)

type Generator struct{}

func NewGenerator() *Generator {
	return &Generator{}
}

func formatHyprlandColor(hexStr string) string {
	if hexStr == "" {
		return "rgba(00000000)"
	}
	if strings.HasPrefix(hexStr, "#") {
		hexStr = hexStr[1:]
	}

	r, g, b, a := "00", "00", "00", "ff"
	if len(hexStr) >= 6 {
		r = hexStr[0:2]
		g = hexStr[2:4]
		b = hexStr[4:6]
	}
	if len(hexStr) == 8 {
		a = hexStr[6:8]
	}

	return fmt.Sprintf("rgba(%s%s%s%s)", r, g, b, a)
}

func parseColorString(str string) (string, string) {
	if str == "" {
		return "", ""
	}
	parts := strings.Fields(str)
	var colors []string
	var angle string

	for _, part := range parts {
		if strings.HasSuffix(part, "deg") {
			angle = part
			continue
		}
		if strings.HasPrefix(part, "rgb(") || strings.HasPrefix(part, "rgba(") {
			colors = append(colors, part)
			continue
		}
		if strings.HasPrefix(part, "#") || len(part) == 6 || len(part) == 8 {
			colors = append(colors, formatHyprlandColor(part))
		}
	}

	colorStr := strings.Join(colors, " ")
	return colorStr, angle
}

func (g *Generator) GenerateAppearance(config ipc.ConfigAppearance) string {
	var out strings.Builder

	out.WriteString("# ▄    ▄▄▄  ▄▄ ▄▄  ▄▄▄▄ ▄▄▄▄▄▄ ▄▄    \n")
	out.WriteString("#  ▀▄ ██▀██ ▀█▄█▀ ██▀▀▀   ██   ██    \n")
	out.WriteString("# ▄▀  ██▀██ ██ ██ ▀████   ██   ██▄▄▄ \n\n")

	out.WriteString("general {\n")
	if config.Gaps != nil {
		if config.Gaps.Inner != nil {
			out.WriteString(fmt.Sprintf("    gaps_in = %d\n", *config.Gaps.Inner))
		}
		if config.Gaps.Outer != nil {
			out.WriteString(fmt.Sprintf("    gaps_out = %d\n", *config.Gaps.Outer))
		}
	}
	if config.Border != nil {
		if config.Border.Width != nil {
			out.WriteString(fmt.Sprintf("    border_size = %d\n", *config.Border.Width))
		}
		if config.Border.ActiveColor != nil {
			colors, angle := parseColorString(*config.Border.ActiveColor)
			if colors != "" {
				if angle != "" {
					colors += " " + angle
				}
				out.WriteString(fmt.Sprintf("    col.active_border = %s\n", colors))
			}
		}
		if config.Border.InactiveColor != nil {
			colors, angle := parseColorString(*config.Border.InactiveColor)
			if colors != "" {
				if angle != "" {
					colors += " " + angle
				}
				out.WriteString(fmt.Sprintf("    col.inactive_border = %s\n", colors))
			}
		}
	}
	if config.Layout != nil && *config.Layout != "" {
		out.WriteString(fmt.Sprintf("    layout = %s\n", *config.Layout))
	}
	out.WriteString("}\n\n")

	out.WriteString("decoration {\n")
	if config.Border != nil && config.Border.Rounding != nil {
		out.WriteString(fmt.Sprintf("    rounding = %d\n", *config.Border.Rounding))
	}
	if config.Opacity != nil {
		if config.Opacity.Active != nil {
			out.WriteString(fmt.Sprintf("    active_opacity = %.2f\n", *config.Opacity.Active))
		}
		if config.Opacity.Inactive != nil {
			out.WriteString(fmt.Sprintf("    inactive_opacity = %.2f\n", *config.Opacity.Inactive))
		}
	}

	if config.Shadow != nil && config.Shadow.Enabled != nil {
		out.WriteString("    shadow {\n")
		out.WriteString(fmt.Sprintf("        enabled = %v\n", *config.Shadow.Enabled))
		if config.Shadow.Size != nil {
			out.WriteString(fmt.Sprintf("        range = %d\n", *config.Shadow.Size))
		}
		if config.Shadow.Color != nil {
			colorStr, _ := parseColorString(*config.Shadow.Color)
			if colorStr != "" {
				out.WriteString(fmt.Sprintf("        color = %s\n", colorStr))
			}
		}
		out.WriteString("    }\n")
	}

	out.WriteString("    blur {\n")
	if config.Blur != nil {
		if config.Blur.Enabled != nil {
			enabled := "false"
			if *config.Blur.Enabled {
				enabled = "true"
			}
			out.WriteString(fmt.Sprintf("        enabled = %s\n", enabled))
		}
		if config.Blur.Size != nil {
			out.WriteString(fmt.Sprintf("        size = %d\n", *config.Blur.Size))
		}
		if config.Blur.Passes != nil {
			out.WriteString(fmt.Sprintf("        passes = %d\n", *config.Blur.Passes))
		}
	}
	out.WriteString("    }\n")
	out.WriteString("}\n\n")

	if config.Animations != nil && config.Animations.Enabled != nil {
		enabled := "false"
		if *config.Animations.Enabled {
			enabled = "true"
		}
		out.WriteString("animations {\n")
		out.WriteString(fmt.Sprintf("    enabled = %s\n", enabled))
		if enabled == "true" {
			out.WriteString("\n")
			out.WriteString("    bezier = myBezier, 0.4, 0.0, 0.2, 1.0\n")
			out.WriteString("    animation = windows, 1, 2.5, myBezier, popin 80%\n")
			out.WriteString("    animation = border, 1, 2.5, myBezier\n")
			out.WriteString("    animation = fade, 1, 2.5, myBezier\n")
			out.WriteString("    animation = workspaces, 1, 2.5, myBezier, slidefade 20%\n")
		}
		out.WriteString("}\n")
	}

	return out.String()
}

func formatModifiers(mods []string) string {
	if len(mods) == 0 {
		return ""
	}
	return strings.Join(mods, " ")
}

// convertMatchFormat converts "class:^(value)$" to "match:class = value"
func convertMatchFormat(match string) string {
	if match == "" {
		return ""
	}
	parts := strings.SplitN(match, ":", 2)
	if len(parts) < 2 {
		return "match:" + match
	}
	prop := parts[0]
	value := parts[1]
	value = strings.TrimPrefix(value, "^")
	value = strings.TrimSuffix(value, "$")
	value = strings.Trim(value, "()")
	return "match:" + prop + " = " + value
}

func (g *Generator) GenerateKeybinds(config ipc.ConfigKeybinds) string {
	var out strings.Builder
	out.WriteString("# Generated by axctl ConfigGenerator (Keybinds)\n")
	out.WriteString("# Do not edit manually!\n\n")

	addBind := func(kb ipc.Keybind, comment string) {
		if !kb.Enabled || kb.Key == "" {
			return
		}
		mod := formatModifiers(kb.Modifiers)
		dispatcher := kb.Dispatcher
		if dispatcher == "" {
			dispatcher = "exec"
		}
		arg := kb.Argument

		bindKw := "bind"
		if strings.HasPrefix(strings.ToLower(kb.Key), "mouse:") {
			bindKw = "bindm"
		}

		mod = strings.ReplaceAll(mod, "SUPER", "SUPER")
		mod = strings.ReplaceAll(mod, "CTRL", "CTRL")
		mod = strings.ReplaceAll(mod, "ALT", "ALT")
		mod = strings.ReplaceAll(mod, "SHIFT", "SHIFT")

		line := fmt.Sprintf("%s = %s, %s, %s", bindKw, mod, kb.Key, dispatcher)
		if strings.TrimSpace(arg) != "" {
			line += fmt.Sprintf(", %s", arg)
		}
		if comment != "" {
			line += fmt.Sprintf(" # %s", comment)
		}
		out.WriteString(line + "\n")
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

	return out.String()
}

func (g *Generator) GenerateWindowRules(rules []ipc.WindowRule) string {
	var out strings.Builder
	out.WriteString("# Generated by axctl ConfigGenerator (Window Rules)\n")
	out.WriteString("# Do not edit manually!\n\n")

	for _, r := range rules {
		useBlockSyntax := r.Float != nil || r.NoBlur != nil || r.NoShadow != nil ||
			r.Rounding != nil || r.BorderSize != nil || r.Pin != nil ||
			r.Fullscreen != nil || r.IdleInhibit != nil || r.NoScreenShare != nil ||
			r.Move != nil || r.Size != nil

		if useBlockSyntax && r.Match != "" {
			// For named rules (with Name set), use block syntax with name first
			if r.Name != "" {
				out.WriteString("windowrule {\n")
				out.WriteString(fmt.Sprintf("    name = %s\n", r.Name))
				matchStr := convertMatchFormat(r.Match)
				out.WriteString(fmt.Sprintf("    %s\n", matchStr))

				if r.Float != nil && *r.Float {
					out.WriteString("    float = on\n")
				}
				if r.NoBlur != nil && *r.NoBlur {
					out.WriteString("    no_blur = on\n")
				}
				if r.NoShadow != nil && *r.NoShadow {
					out.WriteString("    no_shadow = on\n")
				}
				if r.Rounding != nil {
					out.WriteString(fmt.Sprintf("    rounding = %d\n", *r.Rounding))
				}
				if r.BorderSize != nil {
					out.WriteString(fmt.Sprintf("    border_size = %d\n", *r.BorderSize))
				}
				if r.Pin != nil && *r.Pin {
					out.WriteString("    pin = on\n")
				}
				if r.Fullscreen != nil && *r.Fullscreen {
					out.WriteString("    fullscreen = on\n")
				}
				if r.IdleInhibit != nil && *r.IdleInhibit {
					out.WriteString("    idle_inhibit = on\n")
				}
				if r.NoScreenShare != nil && *r.NoScreenShare {
					out.WriteString("    no_screen_share = on\n")
				}
				if r.Move != nil && *r.Move != "" {
					out.WriteString(fmt.Sprintf("    move = %s\n", *r.Move))
				}
				if r.Size != nil && *r.Size != "" {
					out.WriteString(fmt.Sprintf("    size = %s\n", *r.Size))
				}
				out.WriteString("}\n\n")
			} else {
				// For anonymous rules (no Name), use single-line syntax
				// Format: windowrule = rule1 = val1, rule2 = val2, match:prop = val
				var props []string

				if r.Float != nil && *r.Float {
					props = append(props, "float = on")
				}
				if r.NoBlur != nil && *r.NoBlur {
					props = append(props, "no_blur = on")
				}
				if r.NoShadow != nil && *r.NoShadow {
					props = append(props, "no_shadow = on")
				}
				if r.Rounding != nil {
					props = append(props, fmt.Sprintf("rounding = %d", *r.Rounding))
				}
				if r.BorderSize != nil {
					props = append(props, fmt.Sprintf("border_size = %d", *r.BorderSize))
				}
				if r.Pin != nil && *r.Pin {
					props = append(props, "pin = on")
				}
				if r.Fullscreen != nil && *r.Fullscreen {
					props = append(props, "fullscreen = on")
				}
				if r.IdleInhibit != nil && *r.IdleInhibit {
					props = append(props, "idle_inhibit = on")
				}
				if r.NoScreenShare != nil && *r.NoScreenShare {
					props = append(props, "no_screen_share = on")
				}
				if r.Move != nil && *r.Move != "" {
					props = append(props, fmt.Sprintf("move = %s", *r.Move))
				}
				if r.Size != nil && *r.Size != "" {
					props = append(props, fmt.Sprintf("size = %s", *r.Size))
				}

				matchStr := convertMatchFormat(r.Match)
				props = append(props, matchStr)

				out.WriteString("windowrule = " + strings.Join(props, ", ") + "\n\n")
			}
		} else if r.Match != "" && r.Rule != "" {
			out.WriteString(fmt.Sprintf("windowrulev2 = %s, %s\n", r.Rule, r.Match))
		}
	}

	return out.String()
}

func (g *Generator) GenerateLayerRules(rules []ipc.LayerRule) string {
	var out strings.Builder
	out.WriteString("# Generated by axctl ConfigGenerator (Layer Rules)\n")
	out.WriteString("# Do not edit manually!\n\n")

	for _, r := range rules {
		if r.Namespace == "" {
			continue
		}

		// Use single-line syntax for layer rules (no Name field exists)
		// Format: layerrule = rule1 = val1, rule2 = val2, match:namespace = value
		var props []string

		if r.NoAnim != nil && *r.NoAnim {
			props = append(props, "no_anim = on")
		}
		if r.Blur != nil && *r.Blur {
			props = append(props, "blur = on")
		}
		if r.BlurPopups != nil && *r.BlurPopups {
			props = append(props, "blur_popups = on")
		}
		if r.IgnoreAlpha != nil && *r.IgnoreAlpha {
			props = append(props, "ignore_alpha = on")
		}
		if r.IgnoreZeroAlpha != nil && *r.IgnoreZeroAlpha {
			props = append(props, "ignore_zero_alpha = on")
		}
		if r.NoShadow != nil && *r.NoShadow {
			props = append(props, "no_shadow = on")
		}
		if r.IgnoreAlphaValue != nil {
			props = append(props, fmt.Sprintf("ignore_alpha = %.2f", *r.IgnoreAlphaValue))
		}

		matchStr := fmt.Sprintf("match:namespace = %s", r.Namespace)
		props = append(props, matchStr)

		out.WriteString("layerrule = " + strings.Join(props, ", ") + "\n\n")
	}

	return out.String()
}

func (g *Generator) GenerateStartup(exec []string, execOnce []string) string {
	if len(exec) == 0 && len(execOnce) == 0 {
		return ""
	}

	var out strings.Builder
	out.WriteString("# ▄    ▄▄▄  ▄▄ ▄▄  ▄▄▄▄ ▄▄▄▄▄▄ ▄▄    \n")
	out.WriteString("#  ▀▄ ██▀██ ▀█▄█▀ ██▀▀▀   ██   ██    \n")
	out.WriteString("# ▄▀  ██▀██ ██ ██ ▀████   ██   ██▄▄▄ \n\n")
	for _, cmd := range execOnce {
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		out.WriteString(fmt.Sprintf("exec-once = %s\n", cmd))
	}
	for _, cmd := range exec {
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		out.WriteString(fmt.Sprintf("exec = %s\n", cmd))
	}
	return out.String()
}
