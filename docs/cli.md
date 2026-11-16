# CLI reference

## Syntax

```text
axctl [--version|-v|version]
axctl [-c <path>] daemon
axctl subscribe
axctl <category> <action> [arguments...]
```

`-c` only affects the daemon and must appear before `daemon`. Run `axctl` with
no arguments to see the built-in summary. The CLI formats objects and lists as
indented JSON; scalar results are printed without quotes. A normal successful
operation prints `Success`.

## Windows

IDs are opaque strings. If an action accepts an optional ID and it is omitted,
it usually acts on the active window.

```sh
axctl window list
axctl window active
axctl window focus 0x123abc
axctl window focus-dir l             # l, r, u, d
axctl window close [id]
axctl window move r [id]
axctl window resize 1200 800 [id]
axctl window toggle-floating [id]
axctl window fullscreen 1 [id]
axctl window maximize 1 [id]
axctl window fake-fullscreen 1 [id]
axctl window pin 1 [id]
axctl window toggle-group [id]
axctl window group-nav f              # f/next or b/prev depending on backend
axctl window layout-prop scroller_proportion +0.1 [id]
axctl window move-pixel 100 80 [id]
axctl window move-to-workspace-silent 3 [id]
```

`fullscreen`, `maximize`, `fake-fullscreen`, and `pin` accept `1` to enable and
`0` to disable. `move` and `focus-dir` use `l`, `r`, `u`, or `d`. Size and
position are expressed in backend-defined pixels; see [Backends](compositors.md)
for differences. `layout-prop` is currently used only for Mango scroller layout
proportions.

## Workspaces and monitors

```sh
axctl workspace list
axctl workspace active
axctl workspace switch 3
axctl workspace switch r+1
axctl workspace move-to 4 [window-id]
axctl workspace toggle-special [name]

axctl monitor list
axctl monitor focus DP-1
axctl monitor move-to DP-1 [window-id]
axctl monitor set-dpms DP-1 0
```

To move a window silently, use `axctl window
move-to-workspace-silent <workspace> [window-id]`. Workspace IDs and semantics
may vary; Mango uses composite IDs of the form `<monitor>:<tag>`.

## Layouts and overview

```sh
axctl layout list
axctl layout current
axctl layout set scroller
axctl layout next                 # wraps by default
axctl layout prev 0               # does not wrap at the end
axctl overview toggle
```

`layout list` reports `compositor` and `source` (`dynamic` or `static`). niri's
list is static and `set` is not supported. `overview toggle` is implemented on
niri and Mango, but not Hyprland.

## Configuration and keybinds

```sh
axctl config get gaps.inner
axctl config set gaps.inner 8
axctl config batch '{"gaps.inner":8,"gaps.outer":12,"blur.enabled":true}'
axctl config apply '{"appearance":{"gaps":{"inner":8}}}'
axctl config raw-batch 'keyword general:gaps_in 8'
axctl config get-animations
axctl config bind-key SUPER Return 'exec kitty'
axctl config unbind-key SUPER Return
axctl config keybinds-batch '{"binds":[{"modifiers":["SUPER"],"key":"Return","dispatcher":"exec","argument":"kitty","enabled":true}],"unbinds":[]}'
axctl config reload
```

`config set` receives its value as CLI text; for `config batch`, JSON preserves
the value types. `config apply` receives a string containing a `ConfigUniversal`
object, not a file name. `raw-batch` is Hyprland-specific. Interactive changes
are not persisted to TOML: edit the config file to preserve them across daemon
restarts. `config reload` reloads the compositor's root config; it does not
read the TOML file again by itself.

## System and idle

```sh
axctl system execute 'notify-send hello'
axctl system get-cursor-position
axctl system switch-keyboard-layout [next|prev]
axctl system set-keyboard-layouts 'us,es' 'altgr-intl,'
axctl system get-capabilities
axctl system get-compositor
axctl system keymon-status
axctl system exit

axctl system idle-inhibit 1
axctl system idle-wait 30000
axctl system resume-wait 30000
axctl system is-idle 30000
axctl system input-idle-wait 30000
axctl system input-resume-wait 30000
axctl system is-input-idle 30000
axctl system is-inhibited

axctl system idle-monitor-create [timeout_ms] [respect_inhibitors 0|1] [enabled 0|1]
axctl system idle-monitor-update <id> [timeout_ms] [respect_inhibitors 0|1] [enabled 0|1]
axctl system idle-monitor-get <id>
axctl system idle-monitor-destroy <id>
axctl system idle-inhibitor-create [enabled 0|1]
axctl system idle-inhibitor-set <id> <0|1>
axctl system idle-inhibitor-get <id>
axctl system idle-inhibitor-destroy <id>
axctl system inhibit-system <0|1>
axctl system is-system-inhibited
axctl system app-inhibit-check [pattern...]
axctl system media-inhibit-check
```

Timeouts are in milliseconds. `idle-wait` and `resume-wait` respect idle
protocol inhibitors; `input-*` observes physical input and ignores them.
`idle-inhibit` manages a persistent compatibility Wayland inhibitor.
`inhibit-system` is different: it uses `systemd-inhibit` to block system sleep
or idle. The wait commands are blocking.

## Dark mode and brightness

```sh
axctl darkmode status
axctl darkmode on
axctl darkmode off
axctl darkmode toggle

axctl brightness list [monitor]
axctl brightness get <monitor>
axctl brightness set <monitor> <0..1>
axctl brightness set <0..1>          # all devices
axctl brightness adjust <monitor> <delta>
axctl brightness adjust <delta>      # all devices
axctl brightness save [monitor]
axctl brightness restore [monitor]
```

Brightness is normalized to a value between `0` and `1`; `adjust` accepts a
positive or negative delta. Common names include `backlight`,
`backlight-<device>`, and `ddc-<bus>`. See [System services](services.md) for
permissions, dependencies, and persistence.

## Applications and icons

```sh
axctl application list
axctl application search firefox
axctl application get org.mozilla.firefox.desktop
axctl application top
axctl application launch org.mozilla.firefox.desktop
axctl application launch org.mozilla.firefox.desktop <action-id>
axctl icon resolve firefox [48]
```

`application top` returns the full usage ranking from the CLI; a limit can
only be specified through IPC. The RPC also supports identity resolution and
explicit usage recording.

## Notifications and events

```sh
axctl notification list
axctl notification history
axctl notification active
axctl notification dismiss <id>
axctl notification close <id>
axctl notification delete <id>
axctl subscribe
```

`close` and `dismiss` remove a notification from the active list but keep it in
history. `delete` also removes it from history. The CLI does not expose every
notification method or any tray methods; they are available through the IPC
API documented in [IPC API](ipc-api.md).
