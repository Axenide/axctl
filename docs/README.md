# axctl documentation

This documentation describes axctl's implemented behavior: the daemon, CLI,
local protocol, data model, configuration, and supporting services.
Compositor-specific behavior is called out explicitly; the same command does
not necessarily have identical semantics across all backends.

## Contents

- [Architecture and lifecycle](architecture.md): compositor detection, socket,
  security, cache, and request flow.
- [CLI reference](cli.md): available commands, arguments, and examples.
- [IPC API](ipc-api.md): message format, methods, parameters, and events.
- [TOML configuration](configuration.md): configuration files, imports, rules,
  keybinds, and per-compositor config generation.
- [Data model](data-model.md): windows, workspaces, monitors, layouts, and
  normalized identifiers.
- [Backends](compositors.md): differences and unsupported operations in
  Hyprland, niri, and Mango.
- [System services](services.md): applications, icons, notifications, tray,
  brightness, idle, dark mode, and modifier-alone binds.
- [JSON data schema](wayland_schema.json): structural schema for shared state
  objects; it does not define the full RPC method catalog.

## Quick start

```sh
axctl daemon
axctl window list
axctl workspace active
axctl subscribe
```

The daemon must remain running during normal use. Most CLI commands are daemon
clients; they do not connect directly to the compositor's IPC socket.

## Scope

axctl provides a common layer for Hyprland, niri, and Mango, along with local
services that do not depend on a compositor. The common layer does not hide
differences that lack a direct equivalent: some methods return `feature not
supported`, some lists use static introspection, and some operations are blind
toggles on a backend. Check [Backends](compositors.md) before relying on an
operation in portable scripts.
