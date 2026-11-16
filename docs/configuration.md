# TOML configuration

## File location and loading

The preferred path is:

```text
$XDG_CONFIG_HOME/axctl/config.toml
```

If `XDG_CONFIG_HOME` is not set, this normally resolves to
`~/.config/axctl/config.toml`. If that file does not exist, axctl tries the
compatibility file `~/.local/share/ambxst/axctl.toml`. You can also specify a
different file with `axctl -c <path> daemon`.

All sections are optional. If no TOML file exists, the daemon runs without
generating config. The daemon loads the file at startup if it exists and watches
the main file and its resolved imports. Invalid TOML or a config generation
error is reported in the logs; do not assume the new state was applied.

## Imports and precedence

`include` accepts paths relative to the file containing the import and glob
patterns:

```toml
include = ["keybinds.toml", "rules/*.toml"]
```

Imports are processed in order: later imports take precedence over earlier
ones for scalar fields, and values in the main file take precedence over all
imports. Keybinds, window rules, layer rules, and startup commands are
concatenated. Appearance fields are merged by property, and layout maps merge
their keys. Import depth is limited to 10; cycles and imports that fail are
reported as warnings and skipped.

Relative targets are resolved against the directory of the main TOML file
selected for the daemon (`-c` or the detected path), including when `[target]`
comes from an imported file.

## Complete example

```toml
[target]
hyprland = "~/.config/hypr/axctl.generated.conf"
niri = "~/.config/niri/axctl.generated.kdl"
mango = "~/.config/mango/axctl.generated.conf"

[general]
layout = "scroller"

[appearance.gaps]
inner = 6
outer = 12

[appearance.border]
width = 2
rounding = 8
active_color = "#89b4fa"
inactive_color = "#45475a"

[appearance.opacity]
active = 1.0
inactive = 0.92

[appearance.blur]
enabled = true
size = 8
passes = 2

[appearance.shadow]
enabled = true
size = 8
color = "#000000aa"

[appearance.animations]
enabled = true
workspace_style = "slidefade 20%"

[input.keyboard]
layouts = "us,es"
variants = "altgr-intl,"

[[keybinds]]
modifiers = ["SUPER"]
key = "Return"
dispatcher = "exec"
argument = "kitty"
enabled = true

[[window_rules]]
match = "class:firefox"
rule = "opacity 0.95"
action = ""

[[window_rules]]
match = "class:kitty"
float = true
size = "900x700"
move = "center"

[[layer_rules]]
namespace = "^waybar$"
blur = true
no_shadow = true

[startup]
exec-once = ["waybar", "mako"]
exec = "some-command"
```

The first rule shows the legacy `rule`/`action` format; the second uses
granular TOML fields. The supported granular properties are listed below.

## Sections and fields

### `[target]`

| Key | Purpose |
|---|---|
| `hyprland` | Path for the generated Hyprland config file. |
| `niri` | Path for the generated niri KDL file. |
| `mango` | Path for the generated Mango `.conf` file. |

Accepts `~`, `~/...`, absolute paths, or paths relative to the main TOML file's
directory. For Hyprland, the extension determines the sidecar: `.conf` also
generates `.lua`, `.lua` also generates `.conf`, and no extension generates
both. Targets for inactive compositors are rendered only when their `target`
key is set. The active compositor's config is written even if no target is
declared.

### `[general]`

- `layout`: initial layout; Hyprland and Mango can represent it. niri uses
  continuous scrolling and does not allow it to be changed with this setting.
- `workspace_layouts_enabled`: enables explicit layout rules per tag.
- `workspace_layouts`: a `workspace → layout` map, useful for Mango. The
  generator accepts `"*"` for all tags or `"<monitor>:<tag>"` for a specific
  tag, where the tag is between 1 and 31. Example:

  ```toml
  [general.workspace_layouts]
  "*" = "scroller"
  "DP-1:2" = "tile"
  "HDMI-A-1:3" = "grid"
  ```

  Mango emits the wildcard first, if present in the map, followed by per-tag
  rules in key order. niri and Hyprland currently ignore this map.

### `[appearance]`

- `[appearance.gaps]`: `inner`, `outer` (integers).
- `[appearance.border]`: `width`, `rounding` (integers), `active_color`,
  `inactive_color` (color strings).
- `[appearance.opacity]`: `active`, `inactive` (numbers).
- `[appearance.blur]`: `enabled` (bool), `size`, `passes` (integers).
- `[appearance.shadow]`: `enabled` (bool), `size` (integer), `color` (string).
- `[appearance.animations]`: `enabled` (bool), `workspace_style` (string;
  for example, `slidefade 20%` or `slidefadevert`).

The representation is shared, but not every key can be implemented by every
compositor. In niri, for example, generic outer gaps, opacity, blur, and
shadows are not translated; the generator leaves comments in the KDL file.
See [Backends](compositors.md).

### `[input.keyboard]`

- `layouts`: comma-separated XKB layouts, for example `"us,es"`.
- `variants`: comma-separated XKB variants; preserve trailing commas when
  they represent an empty variant, for example `"altgr-intl,"`.

Variants are applied when the TOML file is loaded if `layouts` is not empty.
niri does not support `SetKeyboardLayouts`; Mango updates its XKB options.

### `[[keybinds]]`

Each entry has:

- `modifiers`: a list, for example `["SUPER", "SHIFT"]`.
- `key`: key/trigger in the syntax accepted by the compositor.
- `dispatcher`: action; common examples include `exec`, `fullscreen`,
  `togglefloating`, `killactive`, `movefocus`, `movewindow`, and `workspace`.
- `argument`: dispatcher argument string; use `""` when empty.
- `enabled`: bool. `false` omits the generated bind.
- `flags`: optional string, mainly for compositor-specific flags.

The generators translate modifier, key, and dispatcher names. niri skips
combinations it cannot represent and leaves a comment. A modifier-alone bind
(for example, `key = "Super_L"` and `modifiers = ["SUPER"]`) uses a native
release bind on Hyprland/Mango and axctl's evdev monitor on niri; see
[Services](services.md) for requirements.

### `[[window_rules]]`

There are two forms:

1. Legacy: `match`, `rule`, `action` (strings). Valid example:

   ```toml
   [[window_rules]]
   match = "class:firefox"
   rule = "opacity 0.95"
   action = ""
   ```

2. Granular: `match` plus any of these optional fields:
   `float`, `no_blur`, `no_shadow`, `rounding`, `border_size`, `pin`,
   `fullscreen`, `idle_inhibit`, `no_screen_share`, `move`, `size`.
   `move` accepts compositor-specific formats such as `"100,100"` or
   `"center"`; `size` accepts formats such as `"800x600"` or `"auto"`.

The match is usually expressed as `class:...`, `title:...`, or another
property recognized by the corresponding generator. Conversion and available
properties vary by compositor; Mango emits comments for fields it cannot
convert. The TOML structure does not expose a `name` field for named rules,
although the internal IPC structure supports one.

### `[[layer_rules]]`

Each entry uses `namespace` and may declare `no_anim`, `blur`, `blur_popups`,
`ignore_alpha`, `no_shadow`, `ignore_zero_alpha`, `ignore_alpha_value`, and
`place_within_backdrop`. The last property is specific to niri. Other
generators may ignore properties and leave a note in the generated file.

### Startup commands

`[startup]` contains `exec` and `exec-once`. The root-level `exec` and
`exec-once` keys are also accepted. Each accepts a string or a list of strings.
`exec-once` represents one-time startup commands; `exec` represents recurring
startup commands according to the generated backend format. In JSON IPC, the
equivalent key is serialized as `exec_once`.

## Generated files and integration

Default paths:

| Compositor | Files |
|---|---|
| Hyprland | `$XDG_CONFIG_HOME/hypr/axctl.generated.conf` and `axctl.generated.lua` |
| niri | `$XDG_CONFIG_HOME/niri/axctl.generated.kdl` |
| Mango | `$XDG_CONFIG_HOME/mango/axctl.generated.conf` |

If `XDG_CONFIG_HOME` is unset, `~/.config` is used. Generated files are
intended to be included from the compositor's root config; the handler reloads
that root file and does not replace its contents with the generated file.

```ini
# Mango, in the root config
source=~/.config/axctl/mango/axctl.generated.conf
```

For niri, the generated KDL file includes an `include` directive in its header
that must be added to the main config. For Hyprland, add a `source` directive
for the generated file to the root config (or integrate the Lua sidecar using
the Lua config system already used by the session). Do not edit generated
files manually; the next config load will overwrite them.

For an inactive compositor, axctl writes files only if a destination is
declared in `[target]`; it does not reload that compositor. This lets you
prepare config for another session without trying to connect to that
compositor.

## Runtime configuration

`Config.Set` and `Config.Batch` are runtime operations; they do not write back
to the TOML file. On Hyprland they update keywords. On niri and Mango,
universal appearance keys are applied to a cached payload and the config file
is regenerated. Mango also accepts several native options. When the daemon
restarts, cached state is rebuilt from TOML or from an explicit `Config.Apply`.

`Config.Reload` only asks the compositor to reload. To change the persistent
source, edit the TOML file; the watcher applies changes when it detects them.
