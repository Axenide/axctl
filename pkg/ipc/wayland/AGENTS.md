# Wayland Protocols & Client

## OVERVIEW
Go port of `wayland-client` and generated wrappers for unstable/ext Wayland protocols (`idle_inhibit`, `ext_idle_notify`). Uses `go-wayland-scanner`.

## STRUCTURE
```
pkg/ipc/wayland/
├── client/              # Core Wayland client, context, and registry
├── protocols/           # XML definitions for Wayland extensions
├── ext_idle_notify_v1/  # Generated bindings for ext-idle-notify-v1
└── idle_inhibit_v1/     # Generated bindings for idle-inhibit-unstable-v1
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Code Generation | `client/doc.go` | Contains `go:generate` logic for core protocol |
| Display Types | `client/common.go` | `WaylandDisplay` and `Proxy` definitions |
| Core Protocols | `client/client.go` | Massive generated file, do not edit |
| Custom Protocols| `protocols/*.xml` | Freedesktop XML descriptors |

## CONVENTIONS
- **Code Generation**: All XML files in `protocols/` are mapped to Go files using `go-wayland-scanner`.

## ANTI-PATTERNS (THIS PROJECT)
- **Manual Edits**: Never manually edit files containing a `//go:generate` header comment (e.g., `client.go`, `ext_idle_notify_v1.go`). Changes will be overwritten.

## COMMANDS
```bash
# Re-generate bindings (requires go-wayland-scanner)
go generate ./pkg/ipc/wayland/client/...
```
