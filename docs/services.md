# System services

In addition to compositor IPC, the daemon hosts supporting services. Some can
run even when no compositor is detected. Query overall status with
`Service.Status`; notifications and tray also expose `Notification.Status` and
`Tray.Status`.

## Application catalog

The catalog reads `.desktop` entries from:

- `$XDG_DATA_HOME/applications` or, if unset, `~/.local/share/applications`.
- Each `$XDG_DATA_DIRS` directory (default:
  `/usr/local/share:/usr/share`) under `applications`.

The first directory wins when an ID is duplicated. Only `Type=Application`
entries are cataloged. The visible list filters `Hidden`, `NoDisplay`,
`TryExec`, `OnlyShowIn`, and `NotShowIn` according to the current desktop.
Localized names are selected using `LC_ALL`, `LC_MESSAGES`, or `LANG`; desktop
actions (`[Desktop Action ...]`) are also exposed.

An entry includes its ID (relative path converted to a desktop ID), name,
description, icon, executable, parsed arguments, categories, keywords, MIME
types, actions, working directory, `StartupWMClass`, `TryExec`, and flags such
as `Terminal`/`DBusActivatable`.

The `icon` field is usually a theme icon name, though a desktop entry may
provide a path. Pass it to `Icon.Resolve` or `Icon.ResolveMany` to get a
resolved path. These methods resolve requested names; there is no API to
enumerate every icon in an icon theme.

### Search, ranking, and launch

- `Application.Search` matches the name, command, executable, comment,
  generic name, and keywords. Exact/prefix name matches rank highest, followed
  by relevance scores from the other fields.
- `Application.Top` sorts by recorded usage.
- `Application.Get` uses the desktop ID; `Application.Resolve` tries the ID,
  `StartupWMClass`, name, and executable without case sensitivity.
- Usage scores decay over time (with a one-week scale constant) and increase
  with the launch count.
- `Application.Launch` attempts D-Bus activation for `DBusActivatable` entries;
  if that fails and `Exec` is present, it runs that command. RPC supports
  desktop actions and URIs. Common field codes `%c`, `%k`, `%%`, `%f`, `%u`,
  `%F`, `%U`, and `%i` are expanded.
- If an entry requires a terminal (`Terminal=true`), `TERMINAL` must name the
  emulator; axctl adds `-e`. `HL_INITIAL_WORKSPACE_TOKEN` is removed from the
  child process environment before launch.
- A successful launch records usage in
  `$XDG_STATE_HOME/axctl/application-usage.json` or
  `~/.local/state/axctl/application-usage.json`.

The catalog is updated by a file watcher. On changes, it emits
`Event.ApplicationsChanged` with an incremented revision; query the catalog
again to get the updated contents.

## Icon resolution

`Icon.Resolve` searches names in the current icon theme and its inheritance
chain, then `hicolor`, and finally fallback locations such as `pixmaps`. It
supports `.png`, `.svg`, and `.xpm`; an existing absolute path is returned
directly. If there is no match, the `path` result is an empty string.

The default size is 48 and the scale is 1. The theme is selected when the
resolver is created, in this order:

1. `QS_ICON_THEME`.
2. `ICON_THEME`.
3. `gsettings get org.gnome.desktop.interface icon-theme`.
4. `hicolor`.

Icon paths are searched under `$XDG_DATA_HOME/icons` or
`~/.local/share/icons`, `~/.icons`, the `icons` directories in `$XDG_DATA_DIRS`,
and `/usr/share/pixmaps`. The resolver caches both hits and misses.
`Icon.Theme` only reports the theme; the current API has no RPC method to
change it.

## D-Bus notifications

The daemon can act as an `org.freedesktop.Notifications` server on the session
bus. Set `AXCTL_NOTIFICATIONS=0` to disable it. If another process already owns
the D-Bus name, the service enters a conflict state and does not replace the
existing owner. Query `Notification.Status` to distinguish `available`,
`disabled`, `conflict: ...`, and `unavailable: ...`.

Notifications are kept in two collections:

- **Active**: visible/actionable notifications, with an expiration timer when
  `timeout > 0`.
- **History**: persistent, limited to 1000 entries. On restart, history is
  loaded but the active list is not reconstructed.

Persistent data:

- History and DND: `$XDG_STATE_HOME/axctl/notifications.json` or
  `~/.local/state/axctl/notifications.json`.
- Images received through D-Bus hints: under
  `$XDG_CACHE_HOME/axctl/notifications/images` or `~/.cache/...`.

Do Not Disturb retains notifications and history but forces `popup=false`. A
matching `replaceKey` replaces the active notification with the same ID and
updates its history entry. Actions emitted by axctl produce
`Event.NotificationActionInvoked`; non-resident notifications are closed when
an action is invoked. `Delete` removes the history entry; `Dismiss`/`Close`
only remove it from the active list; `Clear` closes active notifications and
clears history.

The service advertises the D-Bus capabilities `actions`, `body`, `body-markup`,
`icon-static`, and `persistence`. It may share the standard name with another
daemon, so check its status before relying on it.

## StatusNotifier tray

axctl can host the `org.kde.StatusNotifierWatcher` D-Bus watcher and register
itself as a host. Set `AXCTL_TRAY=0` to disable it. If another watcher already
owns the name, the tray service enters a conflict state.

`Tray.List` returns StatusNotifierItem metadata: ID, service, object path,
category, title, status, icon, tooltip, and menu reference. The
`Tray.Activate`, `SecondaryActivate`, `ContextMenu`, and `Scroll` actions
forward the operation to the item owner. `Tray.Menu` requests the current
D-BusMenu layout, and `Tray.MenuEvent` sends an interaction. Items are removed
when D-Bus owner changes indicate they have disappeared.

Received pixmaps are decoded from the specification's network-order ARGB32
format, converted to PNG, and cached in `$XDG_CACHE_HOME/axctl/tray` or
`~/.cache/axctl/tray`. Registered D-Bus item references are cached in
`$XDG_RUNTIME_DIR/axctl/tray-items.json` so a daemon restart can re-query items
whose owning applications are still running; stale references are discarded.
The tray RPC API has no CLI subcommands; use the methods described in the
[IPC API](ipc-api.md).

## Brightness

Brightness sources are:

- Internal panels under `/sys/class/backlight`, controlled with
  `brightnessctl`.
- External displays detected with `ddcutil detect --brief` and controlled via
  DDC/CI, VCP code `0x10` (requires `ddcutil` and access to the corresponding
  I²C device).

Common IDs:

| ID | Meaning |
|---|---|
| `backlight-<kernel-device>` | A specific internal panel, for example `backlight-amdgpu_bl1`. |
| `backlight` | All internal panels. |
| `ddc-<bus>` | External display associated with the bus, for example `ddc-3`. |
| `all` or an empty string | All devices for set/adjust/save/restore. |

Exposed values are fractions from 0 to 1. Internally they are converted to
percentages; `brightnessctl` does not go below 1% even if 0 is requested, while
DDC supports 0%. `adjust` clamps the result to 0.05..1. Saved values persist as
percentages in `$XDG_CONFIG_HOME/axctl/brightness.tsv` or
`~/.config/axctl/brightness.tsv`. Restoring an ID requires that it was
previously saved.

`Event.BrightnessChanged` events contain the absolute value read after the
operation, not the requested delta.

## Dark mode

`Darkmode.*` runs `gsettings` on `org.gnome.desktop.interface color-scheme`,
using the values `prefer-dark` and `prefer-light`. `gsettings` and that schema
must be available in the session. This does not directly change each
application's internal preferences.

## Idle, inhibition, and application/media detection

The idle manager uses its own Wayland connection and the
`ext_idle_notifier_v1` and `zwp_idle_inhibit_manager_v1` extensions when
available. niri honors only inhibitors attached to visible surfaces; axctl
creates a transparent layer-shell surface for this when layer-shell is
available.

- `IdleWait`/`IsIdle`/monitors with `respect_inhibitors=true` observe session
  idle and respect application inhibitors.
- `InputIdleWait`/`IsInputIdle` and monitors with `respect_inhibitors=false`
  observe physical input and ignore inhibitors.
- `IdleMonitorCreate` is stateful and emits `Event.IdleMonitorChanged` when the
  threshold is crossed.
- The legacy `System.IdleInhibit` method and `IdleInhibitorCreate` resources are
  Wayland inhibitors; `System.InhibitSystem` is different and keeps a
  `systemd-inhibit --what=sleep:idle` process running.
- `AppInhibitCheck` uses Hyprland/niri clients when possible and falls back to
  process inspection. Default patterns include VLC, mpv, Firefox, Chromium,
  Chrome, Brave, Vivaldi, and Steam. On Mango it also checks cached windows.
- `MediaInhibitCheck` inspects `pactl list sink-inputs`, excludes muted/corked
  streams and some audio services, and returns `count` and `apps`.

If the Wayland protocols could not be initialized, the daemon still starts and
logs a warning; idle calls may return an error.

## Modifier-alone binds

An entry such as `SUPER + Super_L` means to run the command when **releasing**
Super, provided no other key or button was pressed while it was held. The
recognized modifier groups are Super, Alt, Ctrl, and Shift.

- **Hyprland and Mango**: the generator writes a native release bind.
- **niri**: the compositor has no release action. axctl reads events from
  `/dev/input/event*` in read-only mode (no grab or uinput) and runs the
  command when the modifier is released without other interaction.

For niri, the daemon user must belong to the `input` group and must log in
again for the new group membership to take effect. The installer attempts to
add the group on non-NixOS distributions; on NixOS, configure
`users.users.<name>.extraGroups`. Verify with:

```sh
id | tr ',' '\n' | grep -w input
axctl system keymon-status
```

The monitor starts when the first bind is registered and opens devices present
at that time. Keyboards added later require a daemon restart. Any key, mouse
button, or wheel input cancels the candidate; pointer movement does not. If no
device can be read, the binds remain disabled and a warning is logged.
