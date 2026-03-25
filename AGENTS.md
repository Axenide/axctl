# PROJECT KNOWLEDGE BASE

**Generated:** 2026-03-25T16:51:00Z
**Commit:** 9fe31a8
**Branch:** dev

## OVERVIEW
Universal IPC daemon for Wayland compositors (Hyprland, Niri, Mango) written in Go. Provides a unified JSON-RPC API for window and workspace management.

## STRUCTURE
```
/
├── main.go         # CLI entry point (at ROOT, non-standard)
├── cmd/idle-monitor/  # Empty, incomplete feature
├── pkg/
│   ├── ipc/        # Core interfaces and compositor adapters
│   │   ├── hyprland/
│   │   ├── niri/
│   │   ├── mango/
│   │   └── wayland/  # Wayland protocol bindings
│   ├── config/     # TOML config parsing
│   └── server/     # JSON-RPC server over Unix Domain Socket
└── go.mod
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| CLI Commands | `main.go` | Entry at ROOT (non-standard) |
| IPC Server | `pkg/server/server.go` | Socket handling and method dispatching |
| Compositor Logic | `pkg/ipc/` | Interfaces and specific adapter implementations |
| State Management | `pkg/ipc/cache.go` | Memory-backed state for rapid queries |

## CODE MAP
| Symbol | Type | Location | Refs | Role |
|--------|------|----------|------|------|
| `Compositor` | Interface | `pkg/ipc/interface.go` | ~10 | Standard API for all adapters |
| `Server` | Struct | `pkg/server/server.go` | ~5 | Orchestrates IPC and event loop |
| `Hyprland` | Struct | `pkg/ipc/hyprland/client.go` | ~3 | Adapter for Hyprland protocol |
| `Niri` | Struct | `pkg/ipc/niri/client.go` | ~3 | Adapter for Niri JSON-RPC |

## CONVENTIONS
- **Absolute Paths**: Always use absolute paths for file/socket operations.
- **JSON-RPC**: Communication between CLI and Daemon follows JSON-RPC over Unix sockets.
- **Fail Gracefully**: Methods return `ErrNotSupported` if a feature isn't available on a specific compositor.

## ANTI-PATTERNS (THIS PROJECT)
- **Direct Socket Access**: CLI must go through the Daemon, not talk to compositors directly.
- **Hardcoded IDs**: IDs can be hexadecimal (Hyprland) or integers (Niri); always treat as strings in the abstraction.
- **Thread Safety**: In `pkg/server/idle.go`, ALWAYS set handler BEFORE unlocking mutex (see line ~295).

## UNIQUE STYLES
- **Adapter Pattern**: Each compositor is a separate package implementing the `Compositor` interface.
- **State Caching**: Read-only queries are served from memory, updated by an event subscriber loop.

## COMMANDS
```bash
# Build
go build -o axctl .

# Start Daemon
./axctl daemon

# Query
./axctl window list
```

## NOTES
- **Hyprland**: Uses `$HYPRLAND_INSTANCE_SIGNATURE` for socket paths.
- **Niri**: Uses `$NIRI_SOCKET` for IPC.
- **Mango**: Uses `/run/user/$UID/mango.sock`.
