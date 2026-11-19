# IPC API

axctl uses a local Unix socket. This document describes the API implemented by
`pkg/server.Server`; it is not the native IPC protocol of Hyprland, niri, or
Mango.

## Framing and format

Each request is a JSON object sent to the socket. One newline-terminated
request per line is recommended:

```json
{"id":1,"method":"Window.List","params":{}}
```

`jsonrpc` is not required in a request. The fields are:

| Field | Type | Purpose |
|---|---|---|
| `id` | any JSON value | Copied to the response. |
| `method` | string | Exact, case-sensitive method name, such as `Window.List`. |
| `params` | object | Method arguments; use `{}` when there are none. |

A successful response looks like this:

```json
{"id":1,"result":[{"id":"42","title":"Editor"}]}
```

Mutations without a payload usually return `{"id":1,"result":"ok"}`.
Errors are strings, not standard JSON-RPC error objects:

```json
{"id":1,"error":"feature not supported on this compositor"}
```

The server accepts multiple sequential requests on the same connection. After
starting a subscription (`System.Subscribe`), that connection is dedicated to
receiving notifications; use another connection for new requests.

## Compositor state

`Window.List`, `Workspace.List`, and `Monitor.List` read from the daemon cache.
`Window.Active` and `Workspace.Active` query the backend directly. The monitor,
window, and workspace lists are initialized at startup and reconciled with
compositor events. Returned objects and their fields are described in the
[Data model](data-model.md).

| Method | Parameters | Result/semantics |
|---|---|---|
| `Window.List` | `{}` | `Window[]` from the cache. |
| `Window.Active` | `{}` | `{ "id": string }`. |
| `Window.Focus` | `{ "id": string }` | Focus the specified ID. |
| `Window.FocusDir` | `{ "direction": "l"\|"r"\|"u"\|"d" }` | Focus in that direction. |
| `Window.Close` | `{ "id"?: string }` | Close a window; an empty or omitted ID resolves to the active window. |
| `Window.Move` | `{ "direction": string, "id"?: string }` | Move in a direction. Valid CLI directions: `l/r/u/d`. |
| `Window.Resize` | `{ "width": int, "height": int, "id"?: string }` | Request a size; units and interpretation depend on the backend. |
| `Window.ToggleFloating` | `{ "id"?: string }` | Toggle floating mode. |
| `Window.Fullscreen` | `{ "state": bool, "id"?: string }` | Request real fullscreen. |
| `Window.Maximize` | `{ "state": bool, "id"?: string }` | Request maximized state; niri implements a blind toggle. |
| `Window.FakeFullscreen` | `{ "state": bool, "id"?: string }` | Report fullscreen state to the client without occupying the output; niri uses a blind toggle. |
| `Window.Pin` | `{ "state": bool, "id"?: string }` | Pin/global state depending on the backend; niri does not support it. |
| `Window.ToggleGroup` | `{ "id"?: string }` | Toggle window groups when supported. |
| `Window.GroupNav` | `{ "direction": string }` | Navigate within a group; use `f`/`b` or backend aliases. |
| `Window.LayoutProp` | `{ "key": string, "value": string, "id"?: string }` | Set a layout property; Mango supports scroller proportions. |
| `Window.MovePixel` | `{ "x": int, "y": int, "id"?: string }` | Move to an absolute position where implemented by the backend. On niri, this applies to floating windows. |
| `Window.MoveToWorkspaceSilent` | `{ "workspace_id": string, "window_id"?: string }` | Move without switching to the workspace when supported. |

If a window `id` is omitted in methods that resolve one, axctl calls
`ActiveWindow`. Unknown IDs, invalid parameters, and compositor limitations are
reported in `error`.

| Method | Parameters | Result/semantics |
|---|---|---|
| `Workspace.List` | `{}` | `Workspace[]` from the cache. |
| `Workspace.Active` | `{}` | Active workspace, or an error if it cannot be determined. |
| `Workspace.Switch` | `{ "id": string }` | Switch workspace/tag; some backends accept `+1`, `r+1`, `-1`, and `r-1`. |
| `Workspace.MoveTo` | `{ "workspace_id": string, "window_id"?: string }` | Move a window (active by default); behavior depends on the backend. |
| `Workspace.ToggleSpecial` | `{ "name"?: string }` | Toggle a special workspace when available. |
| `Monitor.List` | `{}` | `Monitor[]` from the cache. |
| `Monitor.Focus` | `{ "id": string }` | Focus a monitor. |
| `Monitor.MoveTo` | `{ "monitor_id": string, "window_id"?: string }` | Move a window to a monitor. |
| `Monitor.SetDpms` | `{ "monitor_id"?: string, "on": bool }` | Turn DPMS on/off; an empty ID may refer to all monitors on Hyprland. |
| `Overview.Toggle` | `{}` | Toggle overview on niri and Mango. |

## Layouts

| Method | Parameters | Result |
|---|---|---|
| `Layout.List` | `{}` | `{items: Layout[], active?: string, compositor: string, source: string}`. |
| `Layout.Current` | `{}` | Current `Layout` object. |
| `Layout.Set` | `{ "name": string }` | Set the layout if the backend supports it. |
| `Layout.Next` | `{ "wrap"?: bool }` | Advance; `wrap` defaults to `true`. Returns the new `Layout`. |
| `Layout.Prev` | `{ "wrap"?: bool }` | Go back; `wrap` defaults to `true`. Returns the new `Layout`. |

If `wrap=false` and the end of the list is reached, the layout is not changed
and the current layout is returned. A backend may use a static list; check
`source` before assuming all names come from the compositor.

## Configuration and keybinds

| Method | Parameters | Result/semantics |
|---|---|---|
| `Config.Get` | `{ "key": string }` | Returns key-specific JSON with no common schema: Hyprland forwards its raw `j/getoption` object; niri and Mango return cached appearance-key values. Unsupported keys fail. |
| `Config.Set` | `{ "key": string, "value": any }` | Change an option or regenerate config, depending on the backend. |
| `Config.Batch` | `{ "configs": { "key": value, ... } }` | Apply multiple options. niri applies universal appearance settings through its cached generated config. Mango splits universal appearance settings (cached/generated config) from other settings (native runtime batch). Hyprland sends the batch to its runtime API. |
| `Config.Apply` | `{ "payload": string }` | `payload` is a JSON string containing `ConfigUniversal`; writes generated files and reloads. |
| `Config.KeybindsBatch` | `{ "payload": string }` | JSON string containing `{ "binds": Keybind[], "unbinds": KeybindTarget[] }`. |
| `Config.BindKey` | `{ "mods": string, "key": string, "command": string }` | Add a keybind incrementally. Mango writes it to generated config; niri only handles modifier-alone binds through axctl's key monitor. |
| `Config.UnbindKey` | `{ "mods": string, "key": string }` | Remove the keybind if supported. Mango updates generated config; niri only handles modifier-alone binds through axctl's key monitor. |
| `Config.RawBatch` | `{ "command": string }` | Native text batch; Hyprland only. |
| `Config.Reload` | `{}` | Reload the compositor root config; does not reread TOML. |
| `Config.GetAnimations` | `{}` | Raw JSON from Hyprland's `j/animations` endpoint; backend-native and not normalized. Unsupported on niri and Mango. |

A `Keybind` contains `modifiers: string[]`, `key`, `dispatcher`, `argument`,
`enabled: bool`, and optional `flags`. `KeybindTarget` contains `modifiers` and
`key`. Unbinds are processed before binds. Generator behavior and persistence
are described in [TOML configuration](configuration.md).

## `System` methods

### Compositor and input

| Method | Parameters | Result/semantics |
|---|---|---|
| `System.Execute` | `{ "command": string }` | Execute a command through the compositor. |
| `System.GetCursorPosition` | `{}` | `{ "x": int, "y": int }`; not supported by every backend. |
| `System.GetCapabilities` | `{}` | `{ "blur": bool, "shadows": bool, "animations": bool, "rounded_corners": bool, "workspaces_supported": bool, "windows_supported": bool }`. |
| `System.GetCompositor` | `{}` | String: `hyprland`, `niri`, `mango`, or `unknown`. |
| `System.KeymonStatus` | `{}` | `{ "open": bool, "binds": map<string, string>, "devices": string[], "errors": map<string, string> }` for the modifier-alone bind monitor. |
| `System.SwitchKeyboardLayout` | `{ "action"?: string }` | `next`, `prev`, or an index when supported by the backend; omitted means `next`. |
| `System.SetKeyboardLayouts` | `{ "layouts": string, "variants"?: string }` | Set comma-separated XKB layout lists when supported. |
| `System.Exit` | `{}` | Request compositor exit. |

### Idle monitors and inhibitors

All timeouts are in milliseconds. `IdleWait`, `ResumeWait`, `IsIdle`, the
`Input*` methods, and enabling an idle monitor require the Wayland
idle-notifier extension; enabling a Wayland inhibitor requires the idle-inhibit
extension. Calls may fail with `Idle management not supported on this
session`, `idle_notify not supported by compositor`, or
`idle_inhibit not supported by compositor`. `System.InhibitSystem` uses
`systemd-inhibit`, while the app/media checks use compositor, process, or audio
data; all of these RPCs are also unavailable if the daemon could not initialize
its Wayland idle manager.

| Method | Parameters | Result/semantics |
|---|---|---|
| `System.IdleInhibit` | `{ "on": bool }` | Enable/disable the compatibility Wayland inhibitor. |
| `System.IdleWait` | `{ "timeout_ms": uint }` | Block until the session has been idle for this interval; respects inhibitors. |
| `System.ResumeWait` | `{ "timeout_ms": uint }` | Wait for the idle threshold to be crossed, then wait for activity to resume. |
| `System.IsIdle` | `{ "timeout_ms": uint }` | Returns the string `"true"` or `"false"`. |
| `System.InputIdleWait` | `{ "timeout_ms": uint }` | Wait for physical input to become idle, ignoring inhibitors. |
| `System.InputResumeWait` | `{ "timeout_ms": uint }` | Wait for physical activity to resume after idle. |
| `System.IsInputIdle` | `{ "timeout_ms": uint }` | Returns the string `"true"` or `"false"`. |
| `System.IsInhibited` | `{}` | Compatibility Wayland inhibitor state as a boolean string. |
| `System.IdleMonitorCreate` | `{ "timeout_ms"?: uint, "respect_inhibitors"?: bool, "enabled"?: bool }` | Create and return `IdleMonitorState`. Defaults: `0`, `true`, `true`. |
| `System.IdleMonitorUpdate` | `{ "id": uint, "timeout_ms"?: uint, "respect_inhibitors"?: bool, "enabled"?: bool }` | Update the specified fields and return full `IdleMonitorState`. |
| `System.IdleMonitorGet` | `{ "id": uint }` | Return `IdleMonitorState`. |
| `System.IdleMonitorDestroy` | `{ "id": uint }` | Destroy the monitor. |
| `System.IdleInhibitorCreate` | `{ "enabled"?: bool }` | Create a Wayland inhibitor; defaults to `false`; returns `IdleInhibitorState`. |
| `System.IdleInhibitorSet` | `{ "id": uint, "enabled": bool }` | Enable/disable and return `IdleInhibitorState`. |
| `System.IdleInhibitorGet` | `{ "id": uint }` | Return `IdleInhibitorState`. |
| `System.IdleInhibitorDestroy` | `{ "id": uint }` | Destroy the inhibitor. |
| `System.InhibitSystem` | `{ "on": bool }` | Start/stop `systemd-inhibit` to block system sleep/idle. |
| `System.IsSystemInhibited` | `{}` | Returns the string `"true"` or `"false"`. |
| `System.AppInhibitCheck` | `{ "patterns"?: string[] }` | `map<string, true>` from matching application identifiers to `true`; see [Services](services.md). |
| `System.MediaInhibitCheck` | `{}` | `{ "count": int, "apps": string[] (nullable) }` with distinct nonempty application names from sink inputs not marked muted/corked; `count` is the number of names, not streams. |

`IdleMonitorState` is
`{ "id": uint, "enabled": bool, "timeout_ms": uint, "respect_inhibitors": bool, "is_idle": bool }`.
`IdleInhibitorState` is
`{ "id": uint, "enabled": bool }`. `KeymonStatus.binds` maps modifier groups to
commands; `devices` contains device paths and `errors` maps device paths to
error strings.

Monitors and inhibitors live in daemon memory. Their IDs do not survive a
process restart.

## Dark mode and brightness

| Method | Parameters | Result/semantics |
|---|---|---|
| `Darkmode.On` / `Darkmode.Off` | `{}` | Set `prefer-dark` or `prefer-light` using `gsettings`. |
| `Darkmode.Toggle` | `{}` | Toggle the current color scheme. |
| `Darkmode.Status` | `{}` | `{ "dark": bool }`. |
| `Brightness.List` | `{ "monitor"?: string }` | `BrightnessDevice[]`, optionally filtered by ID/name. |
| `Brightness.Get` | `{ "monitor": string }` | `{ "brightness": number }`, normalized to 0..1. |
| `Brightness.Set` | `{ "monitor"?: string, "value": number }` | Absolute value; applies to all devices if monitor is omitted. |
| `Brightness.Adjust` | `{ "monitor"?: string, "delta": number }` | Relative delta; applies to all devices if monitor is omitted. |
| `Brightness.Save` | `{ "monitor"?: string }` | Save the current value; saves all devices if monitor is omitted. |
| `Brightness.Restore` | `{ "monitor"?: string }` | Restore the saved value; restores all saved devices if monitor is omitted. |

Each `BrightnessDevice` is `{ "name": string, "key"?: string, "kind":
"brightnessctl" | "ddcutil", "bus"?: string, "brightness"?: number }`.
`brightness` is normalized to 0..1 and is omitted when the current value cannot
be read; `key` is the canonical monitor identifier used by
`Event.BrightnessChanged`.

`Brightness.Set` and `Brightness.Adjust` emit `Event.BrightnessChanged` for
devices whose values could be read back.

Internal-panel IDs are kernel-derived, but the current `brightnessctl` calls
select by class without passing an individual device name; see [System
services](services.md) for the resulting multi-panel limitation.

## Applications, icons, and service status

| Method | Parameters | Result/semantics |
|---|---|---|
| `Application.List` | `{}` | `Entry[]` of visible `.desktop` catalog entries. |
| `Application.Top` | `{ "limit"?: int }` | `SearchResult[]` ranked by usage; an omitted or non-positive limit returns all entries. |
| `Application.Get` | `{ "id": string }` | `Entry` by desktop ID. |
| `Application.Resolve` | `{ "identity": string }` | `Entry` found by ID, `StartupWMClass`, name, or executable. |
| `Application.Search` | `{ "query": string, "limit"?: int }` | `SearchResult[]` ranked by relevance and usage; an omitted or non-positive limit defaults to 10. |
| `Application.Launch` | `{ "id": string, "actionId"?: string, "uris"?: string[] }` | Launch a desktop entry or action. |
| `Application.RecordUsage` | `{ "id": string }` | Increment the persistent usage record. |
| `Application.Revision` | `{}` | `{ "revision": uint }`; increases when catalog changes are detected. |
| `Icon.Resolve` | `{ "name": string, "size"?: int, "scale"?: int }` | `{ "path": string }`; empty if not found. |
| `Icon.ResolveMany` | `{ "names": string[], "size"?: int, "scale"?: int }` | Map of icon names to paths (missing icons map to an empty string). |
| `Icon.Theme` | `{}` | `{ "name": string }`, the theme detected when the resolver was created. |
| `Service.Status` | `{}` | `{ "apiVersion": 1, "compositor": string, "applications": bool, "icons": bool, "notifications": bool, "tray": bool }`; `applications`/`icons` report initialization, while `notifications`/`tray` are true only when available. |

`Entry` has this JSON shape; optional fields are omitted when empty, and false
boolean fields are omitted:

```ts
type Entry = {
  id: string
  path: string
  name: string
  genericName?: string
  comment?: string
  icon?: string
  exec?: string
  command?: string[]
  categories?: string[]
  keywords?: string[]
  mimeTypes?: string[]
  actions?: ApplicationAction[]
  workingDirectory?: string
  startupWMClass?: string
  tryExec?: string
  dbusActivatable?: boolean
  noDisplay?: boolean
  hidden?: boolean
  terminal?: boolean
  onlyShowIn?: string[]
  notShowIn?: string[]
}

type ApplicationAction = { id: string; name: string; exec: string }
type SearchResult = Entry & { score: number; usageScore: number; totalScore: number }
```

`path` is the desktop-file path.

`categories` is an array parsed from the `.desktop` `Categories` value, whose
entries are separated by semicolons; empty entries are discarded. It is omitted
when there are no categories. `SearchResult` is a flattened `Entry` with three
additional integer fields: `score` (text relevance), `usageScore` (the decaying
usage component), and `totalScore` (the sum used for ranking). `Top` sets
`score` to `0` and `totalScore` equal to `usageScore`.

## Notifications

These methods use the `org.freedesktop.Notifications` D-Bus service hosted by
the daemon. It may be disabled, unavailable, or in conflict with another
notification daemon.

| Method | Parameters | Result/semantics |
|---|---|---|
| `Notification.List` / `Notification.History` | `{}` | `Notification[]` containing persistent history, up to 1000 entries. |
| `Notification.Active` | `{}` | `Notification[]` of active notifications sorted by time. |
| `Notification.Status` | `{}` | `{ "status": string }`; values are `available`, `disabled`, `conflict: <reason>`, `unavailable: <reason>`, or `stopped` before service startup. If the service is absent, the result is `unavailable`. |
| `Notification.Send` | `NotificationSendParams` | `{ "id": uint }`. |
| `Notification.Close` | `{ "id": uint }` | Close as requested by the user; retain history. |
| `Notification.Dismiss` | `{ "id": uint }` | Remove from active notifications; retain history. |
| `Notification.Delete` | `{ "id": uint }` | Close and remove from history. |
| `Notification.Action` | `{ "id": uint, "actionId": string }` | Emit the D-Bus action; a non-resident notification is closed afterward. |
| `Notification.Pause` / `Notification.Resume` | `{ "id": uint }` | Pause/resume its timeout. |
| `Notification.ClearHistory` | `{}` | Clear the history. |
| `Notification.Clear` | `{}` | Close active notifications and clear the history. |
| `Notification.SetDND` | `{ "enabled": bool }` | Persist the Do Not Disturb setting. |
| `Notification.DND` | `{}` | `{ "enabled": bool }`. |

`Notification.Send` accepts:

```ts
type NotificationSendParams = {
  appName?: string
  appIcon?: string
  summary?: string
  body?: string
  image?: string
  urgency?: string
  expireTimeout?: number
  historyPriority?: number
  replaceKey?: string
  ownerToken?: string
  popup?: boolean
  actions?: { identifier: string; text: string }[]
}
```

For `Notification.Send`, urgency accepts `low`/`0`, `normal` (the default), and
`critical`/`2`; other strings are normalized to `normal`. `popup` defaults to
true. A timeout of `0` disables expiration for this IPC method; a negative
value is normalized to 5000 ms. Do not confuse this with the special timeout
value of the standard D-Bus `Notify` method, which has its own defaults.
`historyPriority` and `ownerToken` are stored as metadata but do not currently
affect history ordering, eviction, or ownership behavior.

Notification list entries have this JSON shape. Optional fields are omitted
when empty, or false/zero for booleans/numbers:

```ts
type Notification = {
  id: number
  appName: string
  appIcon?: string
  summary: string
  body: string
  image?: string
  actions?: NotificationAction[]
  urgency: number
  category?: string
  desktopEntry?: string
  replaceKey?: string
  ownerToken?: string
  historyPriority?: number
  time: number
  timeout: number
  resident?: boolean
  transient?: boolean
  popup: boolean
}

type NotificationAction = { identifier: string; text: string }
```

`time` is a Unix timestamp in milliseconds; `timeout` is a duration in
milliseconds. The `urgency` field is a byte; standard values are `0` (low), `1`
(normal), and `2` (critical). `Notification.Send` normalizes urgency to one of
those three values.

## StatusNotifier tray

Item IDs come from `Tray.List`; they are opaque identifiers combining the
service and D-Bus object path.

| Method | Parameters | Result/semantics |
|---|---|---|
| `Tray.List` | `{}` | `TrayItem[]` of registered StatusNotifier items. |
| `Tray.Status` | `{}` | `{ "status": string }`; values include `available`, `disabled`, `conflict: <reason>`, `unavailable: <reason>`, and `stopped` before service startup. If the service is absent, the result is `unavailable`. |
| `Tray.Activate` / `Tray.SecondaryActivate` / `Tray.ContextMenu` | `{ "id": string, "x"?: int, "y"?: int }` | Invoke the item's native method. |
| `Tray.Scroll` | `{ "id": string, "delta": int, "orientation"?: string }` | `orientation` is `horizontal` or, by default, `vertical`. |
| `Tray.Menu` | `{ "id": string }` | `{ "revision": uint, "items": MenuItem[] }`. |
| `Tray.MenuEvent` | `{ "id": string, "menuId": int, "event"?: string }` | Send a menu event; defaults to `clicked`. |

```ts
type TrayItem = {
  id: string
  service: string
  objectPath: string
  category: string
  title: string
  status: string
  iconName?: string
  iconPath?: string
  overlayIcon?: string
  toolTipTitle?: string
  toolTip?: string
  menu?: string
  itemIsMenu: boolean
}

type MenuItem = {
  id: number
  label?: string
  enabled: boolean
  visible: boolean
  iconName?: string
  iconData?: string
  type?: string
  toggleType?: string
  toggleState?: number
  children?: MenuItem[]
}
```

Optional tray fields are omitted when empty (or when `toggleState` is zero).
Invisible menu items are omitted. When available, `iconData` is returned as a
data URI.

## Event subscription

Send `System.Subscribe` and keep the connection open:

```json
{"id":1,"method":"System.Subscribe","params":{}}
```

The normal response (`result: "subscribed"`) is followed by a `State.Dump`
notification. Subsequent messages contain `jsonrpc: "2.0"`, `method`, and
`params`. Events on the regular state-bearing path (compositor, idle-monitor,
and brightness events) also include `state`; standalone application,
notification, and tray events do not. See [Events and state](events.md) for the
event catalog and lifecycle.
