# Data model

axctl's common types are defined in `pkg/ipc/types.go`. The `metadata` fields
preserve compositor-specific information without imposing false semantics on
all backends.

## Window

```json
{
  "id": "0xabc",
  "title": "Editor",
  "app_id": "org.example.Editor",
  "workspace_id": "2",
  "is_focused": true,
  "is_floating": false,
  "is_fullscreen": false,
  "is_hidden": false,
  "metadata": {}
}
```

| Field | Meaning |
|---|---|
| `id` | Opaque window ID. Always a string; it may contain a hexadecimal or decimal value. |
| `title` | Current title, which may be empty. |
| `app_id` | Wayland app ID, X11 class, or another backend-specific identifier. |
| `workspace_id` | Workspace ID axctl treats as the window's primary workspace. |
| `is_focused` | Whether the window has keyboard focus. |
| `is_floating` | Whether the window is outside the tiling layout. |
| `is_fullscreen` | Real fullscreen state as interpreted by the backend; it is not necessarily the same as maximized or fake-fullscreen. |
| `is_hidden` | A hidden state the backend can identify, such as minimized/scratchpad in Mango. |
| `metadata` | Compositor-specific extensions, dimensions, monitor, flags, and other data. May be omitted. |

Do not convert generic IDs to integers or assume `workspace_id` is globally
unique across all backends. Preserve the strings as received.

## Workspace

```json
{
  "id": "2",
  "name": "2",
  "monitor_id": "DP-1",
  "is_active": true,
  "is_empty": false,
  "metadata": {}
}
```

`is_active` identifies the selected or visible workspace/tag according to the
backend. `is_empty` may be approximate or unavailable; for example, Hyprland
and niri currently report `false` in their common records. Do not infer that an
empty workspace does not exist.

Mango has independent tags per monitor. Their IDs are
`<monitor-name>:<tag>`, for example `DP-1:3`; tag `0` represents the special
tag. In a Mango command, a numeric ID without a prefix targets the selected
monitor.

## Monitor

```json
{
  "id": "DP-1",
  "name": "DP-1",
  "description": "",
  "width": 2560,
  "height": 1440,
  "logical_width": 1707,
  "logical_height": 960,
  "refresh_rate": 144,
  "scale": 1.5,
  "is_focused": true,
  "metadata": {}
}
```

`width`/`height` describe the size reported by the backend; compositors do not
all use the same coordinate space. `logical_width` and `logical_height` are
included when available. Hyprland reports physical mode dimensions and axctl
derives the logical extent from the scale; niri reports the logical size when
it exposes it.

## Layouts and capabilities

`Layout` contains `name`, optional `current`, and optional `source` (`dynamic`
or `static`). A `Layout.List` response has this shape:

```json
{
  "items": [{"name":"scroller","current":true,"source":"dynamic"}],
  "active": "scroller",
  "compositor": "mango",
  "source": "dynamic"
}
```

`source=static` means the list comes from a fallback list rather than live
introspection. `Capabilities` reports general flags for blur, shadows,
animations, rounded corners, windows, and workspaces. It is not an exhaustive
matrix of methods; check the backend before assuming a specific action is
available.

## Per-compositor metadata

The set of keys may grow in future versions. These are extensions, not
requirements of the common model.

### Hyprland

- Window: `monitor_id`, `pinned`, `x`, `y`, `width`, `height`.
- Workspace: `focused`.
- Monitor: `active_workspace`, `visible_workspace`, `x`, `y`, `transform`.
- In Hyprland 0.57 or later, a workspace ID may be the string address exposed
  by the compositor; in earlier versions it is the numeric ID converted to a
  string.

### niri

- Window: `monitor_id`, `is_urgent`, `pid`; includes `width`/`height` if niri
  communicates the layout size.
- Workspace: `focused`, `index`, `active_window_id`, `is_urgent`.
- Monitor: `active_workspace`, `make`, `model`, `serial`, `is_custom_mode`,
  `vrr_supported`, `vrr_enabled`, `max_bpc`, `physical_size`, `x`, `y`,
  `transform`.
- niri does not provide absolute window coordinates through this interface, so
  axctl does not synthesize window `x` and `y` values.

### Mango

- Window: `monitor`, `monitor_id`, `is_visible`, `pid`,
  `foreign_toplevel_id`, `tags`, `maximized`, `global`, `overlay`,
  `fake_fullscreen`, `urgent`, `is_group`, `is_group_focus`, `scratchpad`,
  `named_scratchpad`, `xwayland`, `x`, `y`, `width`, `height`, and
  `scroller_proportion`.
- Workspace: `index`, `urgent`, `focused`, `layout`, `layout_symbol`; tag 0
  has `special: true`.
- Monitor: `x`, `y`, `index`, `active`, `layout_index`, `layout_symbol`,
  `overview_open`, `active_tags`, `is_hdr`, `is_vrr`.
- A window may belong to multiple tags. The common `workspace_id` corresponds
  to the primary tag selected by axctl; `metadata.tags` preserves the complete
  assignment.

## Schema

`wayland_schema.json` validates the `StateDump` payload at its root and defines
shared types (including layouts and capabilities) for reuse from `definitions`.
The `metadata` fields are open-ended. This is not an OpenAPI document and does
not contain the contract for each method, argument, error, or event. See the
[IPC API](ipc-api.md) for those contracts.
