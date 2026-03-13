# axctl

axctl is a universal IPC daemon and CLI for Wayland compositors. It normalizes
window, workspace, monitor, layout, config, and system operations across
Hyprland, Niri, and MangoWC via a JSON-RPC API over a Unix socket.

## What it does

- Runs a compositor-aware daemon that auto-detects Hyprland, Niri, or MangoWC
- Exposes a single JSON-RPC interface over `/tmp/axctl-$UID.sock`
- Provides a CLI for window and workspace management, configuration, and system
  helpers
- Watches a TOML config file and applies changes live when possible

## Supported compositors

- Hyprland
- Niri
- MangoWC

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
writing to `/usr/local/bin`.

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
```

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
- `WAYLAND_DISPLAY` for MangoWC fallback
- `NIRI_SOCKET` for Niri
- `HYPRLAND_INSTANCE_SIGNATURE` for Hyprland

The daemon listens on:

`/tmp/axctl-$UID.sock`

## Troubleshooting

- `Error: axctl daemon is already running.`
  - Stop the existing daemon or delete a stale `/tmp/axctl-$UID.sock` and
    restart.
- `Error: no supported compositor detected`
  - Ensure your compositor is running and the expected socket variables are
    set (see Environment and sockets).
- `Error connecting to daemon`
  - Start the daemon with `axctl daemon` and verify the socket exists.

## Development

```bash
go test ./...
```

## License

See `LICENSE`.
