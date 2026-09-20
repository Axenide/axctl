<p align="center">
<img src="./assets/axctl.png" alt="axctl banner" style="width: 50%;" align="center" />
</p>

**axctl** is a universal IPC daemon and CLI for Wayland compositors. It normalizes
window, workspace, monitor, layout, config, and system operations across
Hyprland, Niri, and Mango via a JSON-RPC API over a Unix socket.

## What it does

- Runs a compositor-aware daemon that auto-detects Hyprland, Niri, or Mango
- Exposes a single JSON-RPC interface over `/tmp/axctl-$UID.sock`
- Provides a CLI for window and workspace management, configuration, and system
  helpers
- Watches a TOML config file and applies changes live when possible

## Supported compositors

- Hyprland
- Niri
- Mango

## Architecture overview

- `axctl daemon` detects the active compositor and starts a JSON-RPC server
- `axctl <command>` is a client that sends JSON-RPC requests to the daemon
- The socket lives at `/tmp/axctl-$UID.sock`

## Install

### One-line install

```bash
curl -L get.axeni.de/axctl | sh
```

On NixOS, the installer uses `nix profile add github:Axenide/axctl` instead of
writing to `/usr/local/bin`. On other distros it also adds your user to the
`input` group (needed for modifier-alone binds — see
[Modifier-alone binds](#modifier-alone-binds)).

### Build from source

Requires Go 1.25+.

```bash
go build -o axctl .
./axctl --version
```

### Nix

```bash
nix profile add github:Axenide/axctl

nix build
./result/bin/axctl --version

nix run
```

## Quick start

```bash
# Start the daemon (keep it running)
./axctl daemon

# Query state
./axctl window list

# Stream events
./axctl subscribe
```

> **Modifier-alone binds** (e.g. Super alone opens the launcher) need the
> `input` group. The installer adds it automatically; see
> [Modifier-alone binds](#modifier-alone-binds).

## Usage guide

General form:

```bash
axctl <command> <action> [args]
```

Run `axctl` with no arguments to print the full built-in command list.

### Window

```bash
axctl window list
axctl window active
axctl window focus <id>
axctl window move <l|r|u|d> [id]
axctl window resize <w> <h> [id]
axctl window fullscreen <0|1> [id]
axctl window toggle-floating [id]
```

### Workspace

```bash
axctl workspace list
axctl workspace active
axctl workspace switch <id>
axctl workspace move-to <workspace_id> [window_id]
```

### Overview

```bash
axctl overview toggle
```

Toggles the compositor's overview. Only niri implements it
(`ToggleOverview` IPC action); other compositors return
"feature not supported".

### Monitor

```bash
axctl monitor list
axctl monitor focus <id>
axctl monitor set-dpms <monitor_id> <0|1>
```

### Layout

```bash
axctl layout set <name>
```

### Config

```bash
axctl config get <key>
axctl config set <key> <value>
axctl config batch '{"gaps.inner": 8, "gaps.outer": 12}'
axctl config reload
```

Supported config keys include:

`gaps.inner`, `gaps.outer`, `border.width`, `border.active_color`,
`border.inactive_color`, `opacity.active`, `opacity.inactive`, `blur.enabled`,
`blur.size`, `blur.passes`.

### System

```bash
axctl system get-cursor-position
axctl system switch-keyboard-layout [next|prev]
axctl system set-keyboard-layouts "us,es" "altgr-intl,"
axctl system idle-wait <ms>
axctl system is-idle <ms>
axctl system get-capabilities
axctl system get-compositor
```

### Brightness

Manage backlight (internal panels via `brightnessctl`) and external monitors
(via `ddcutil` over DDC/CI). Values are always in the normalized 0..1 range
unless otherwise noted.

```bash
axctl brightness list                  # all devices, with current 0..1
axctl brightness get <monitor>         # read one device's brightness
axctl brightness set <monitor> <0..1>  # set one device, or omit for all
axctl brightness adjust <monitor> <+/-0..1>
axctl brightness save [monitor]        # snapshot to XDG state dir
axctl brightness restore [monitor]     # reapply saved values
```

Device names follow the convention `backlight-<kernel-dev>` for internal
laptop panels (e.g. `backlight-intel_backlight`, `backlight-amdgpu_bl1`) and
`ddc-<bus>` for external displays (e.g. `ddc-3` for `/dev/i2c-3`). Pass
`backlight` to address every internal panel at once. Saved values persist
across reboots in `$XDG_CONFIG_HOME/axctl/brightness.tsv`.

Successful `set`/`adjust` calls emit an `Event.BrightnessChanged`
notification on the subscribe channel with `{monitor, value}`.

### Notes on IDs

Window, workspace, and monitor IDs are compositor-defined. Treat them as
strings in scripts because Hyprland can use hexadecimal IDs while Niri uses
integers.

## Configuration

The daemon loads TOML from:

`~/.config/axctl/config.toml`

If the file exists, the daemon will load it on startup and watch it for
changes (including any `include` files), applying updates live when supported.

Example snippet:

```toml
[appearance]

  [appearance.gaps]
  inner = 5
  outer = 10

  [appearance.border]
  width = 2
  active_color = "#ff5555"
  inactive_color = "#333333"

[input]

  [input.keyboard]
  layouts = "us,es"
  variants = "altgr-intl,"

[[keybinds]]
modifiers = ["SUPER"]
key = "Return"
dispatcher = "exec"
argument = "kitty"
enabled = true
```

See `pkg/config/example.toml` for the full configuration reference.

## Environment and sockets

The daemon uses these environment variables to detect sockets:

- `XDG_RUNTIME_DIR` for Wayland sockets
- `WAYLAND_DISPLAY` for Mango fallback
- `NIRI_SOCKET` for Niri (optional; auto-discovered from `$XDG_RUNTIME_DIR/niri*.sock` when unset)
- `HYPRLAND_INSTANCE_SIGNATURE` for Hyprland

The daemon listens on:

`/tmp/axctl-$UID.sock`

## Modifier-alone binds

A bind on the modifier key itself (e.g. `Super_L` with modifiers `[SUPER]`
for a Super-alone app launcher) cannot be expressed by compositors without
interfering with modifier+key combos. axctl therefore skips such binds in
every generated compositor config (niri, Hyprland, MangoWC) and detects
modifier-alone presses itself by observing `/dev/input` events (read-only;
no grab, no uinput), running the bound command when the modifier is
released without any other key press in between.

Requirements:

- **The daemon user must be in the `input` group** to read
  `/dev/input/event*`. The installer adds the group automatically; for
  manual or NixOS installs:

  ```sh
  sudo usermod -aG input "$USER"
  ```

  On NixOS, add `input` to `users.users.<name>.extraGroups` instead. Then
  log out and back in (the change does not apply to running sessions).
  Verify with: `id | tr ',' '\n' | grep -w input`.
- Declare the bind normally in the config (key `Super_L` with modifiers
  `[SUPER]`); it is skipped in the generated compositor config and handled
  by the monitor instead. Without the group, the daemon logs a warning and
  modifier-alone binds stay disabled.
- Keyboards connected after the daemon starts are not monitored; restart
  the daemon to pick them up.
- Check the live state with `axctl system keymon-status`.

## Troubleshooting

- `Error: axctl daemon is already running.`
  - Stop the existing daemon or delete a stale `/tmp/axctl-$UID.sock` and
    restart.
- `Error: no supported compositor detected`
  - Ensure your compositor is running and the expected socket variables are
    set (see Environment and sockets).
- `Error connecting to daemon`
  - Start the daemon with `axctl daemon` and verify the socket exists.
- Super-alone binds do nothing
  - The user must be in the `input` group (log back in after adding it).
    Check `axctl system keymon-status`: it lists the registered binds, the
    opened `/dev/input` devices, and any per-device errors.

## Development

```bash
go test ./...
```

## License

See `LICENSE`.
