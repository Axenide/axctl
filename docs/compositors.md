# Backends and differences

All methods are routed through `pkg/ipc.Compositor`, but compositors do not
have identical capabilities. The error `feature not supported on this
compositor` is an expected response for an operation without a native
equivalent.

## Capability summary

| Area | Hyprland | niri | Mango |
|---|---|---|---|
| Windows/workspaces/monitors | Yes | Yes | Yes |
| Focus/move/close/resize/floating | Yes | Yes | Yes |
| Fullscreen | Yes | Yes | Yes |
| Maximize and fake-fullscreen | Yes; fake requires Hyprland Lua dispatchers >= 0.55 | Yes; maximize and fake are blind toggles | Yes |
| Pin/global | Yes | Not supported | Yes, via global toggle |
| Window groups | Yes | Not supported | `group-nav` supported; `toggle-group` unsupported |
| Layout property / scroller proportion | No | No | Yes, only `scroller`/`vertical_scroller` and windows on the selected monitor |
| Overview | No | Yes | Yes |
| Special workspace | Yes, optional name | Not supported | Special tag only, no name |
| Silent move-to-workspace | Yes | Yes | Only within the selected monitor |
| Layouts | Introspection with fallback | Static `scrolling` list; `set` unsupported | Introspection with static fallback |
| Appearance configuration | Runtime keywords and generated config | Generated/cached config; no generic native getter | Mango options and generated/cached config |
| Incremental keybinds | Yes | Regenerates KDL file | Regenerates Mango file |
| `raw-batch` | Yes | No | No |

The capability table does not imply that the same operations have identical
semantics. Relevant details follow.

## Hyprland

- Uses `HYPRLAND_INSTANCE_SIGNATURE` to locate its sockets in the runtime
  directory.
- Window IDs are addresses, usually hexadecimal. Monitor IDs are normalized to
  numeric strings. Starting with Hyprland 0.57, axctl uses the workspace
  address when available.
- For general dispatchers, it selects Lua dispatchers on Hyprland 0.55 and
  later; earlier versions use legacy dispatchers. `fake-fullscreen` has no
  legacy equivalent and returns unsupported when Lua dispatchers are
  unavailable.
- `is_fullscreen` represents real fullscreen only; maximize and fake-fullscreen
  are not exposed as real fullscreen.
- Layout enumeration attempts introspection and falls back to a static list if
  the dynamic list cannot be retrieved.
- `layout-prop` is not implemented.

## niri

- Uses `NIRI_SOCKET` or searches for a `niri*.sock` socket under
  `$XDG_RUNTIME_DIR`.
- Window and workspace IDs are numbers converted to strings; monitors are
  identified by output name.
- `focus-dir` and directional movement preserve niri's column and window
  semantics. To move a specific window, axctl focuses it first and then sends
  the directional action.
- `move-pixel` moves a floating window; niri does not provide absolute
  positioning for a tiled window.
- `SetMaximized` and `SetFakeFullscreen` cannot query the current state in
  niri: they send the corresponding toggle regardless of the requested
  boolean. These operations are not idempotent; do not repeat them to
  "ensure" a state.
- The layout list is static and contains only `scrolling`, marked as current.
  `layout set` is not supported.
- Pinning, groups, layout properties, incremental keybind RPC, `raw-batch`,
  generic `GetConfig`, and cursor position are not supported.
- Appearance settings are materialized as generated KDL. niri uses continuous
  scrolling, so switching to a different layout is not applicable. The
  generated file includes comments for universal options that cannot be
  represented (for example, outer gaps, opacity, blur, and shadows).

## Mango

- Uses `MANGO_INSTANCE_SIGNATURE`; an absolute value is treated as a path,
  while a relative value is resolved under `$XDG_RUNTIME_DIR`. If the Mango
  desktop is detected and the variable is unset, axctl probes active
  `mango-*.sock` sockets.
- Uses one Unix connection per query/dispatch and separate connections for
  watches; it does not launch `mmsg` as a subprocess.
- Window IDs are numeric strings. Workspaces are per-monitor tags
  (`<monitor>:<tag>`), with tags 0 through 31 and tag 0 reserved as special.
- Overview is inferred from whether `active_tags` contains tag 0. The
  visibility of windows on the special tag is reported separately from the
  normal tag set.
- Named special workspaces do not exist. Silent moves to another monitor have
  no equivalent.
- Layouts belong to each tag. `layout set` affects the active tag; the
  workspace list exposes its normalized layout in metadata. TOML configuration
  can define layouts per tag.
- `layout-prop` accepts `proportion` or `scroller_proportion`, only for the
  `scroller` and `vertical_scroller` layouts, and only for windows on the
  selected monitor. A signed value such as `+0.1` is a delta; an unsigned
  value must be between `0.1` and `1`.
- `group-nav` moves focus forward or backward within a group; creating or
  joining groups with `toggle-group` is not implemented.
- The `colresize +conf` preset dispatcher selects the next native preset;
  there is no `-conf`. For relative proportions, generated binds execute
  `axctl window layout-prop ...`, so `axctl` must be in `PATH`.
- Generated configuration uses the current `.conf` option names. Mango has no
  IPC operation for unbinding a keybind; a duplicate bind in the root file
  may conflict with the generated one.

## Values to treat as approximate

- `is_empty` is not consistent across backends: Hyprland and niri report it as
  `false`; Mango uses the client count per tag.
- `Capabilities` is a compact set of general flags, not an inventory of actions
  or a guarantee that a specific config option can be queried or changed.
- Layouts and metadata fields may be static, derived, or absent. Check
  `source` and the values actually returned.
