# PROJECT KNOWLEDGE BASE

**Generated:** 2026-02-03T08:24:00Z
**Commit:** 2abc6d4
**Branch:** master

## OVERVIEW
Universal IPC daemon for Wayland compositors (Hyprland, Niri, Mango) written in Go. Provides a unified JSON-RPC API for window and workspace management.

## STRUCTURE
```
/
├── cmd/axctl/      # CLI entry point and daemon runner
├── pkg/
│   ├── ipc/        # Core interfaces, types, and compositor adapters
│   │   ├── hyprland/
│   │   ├── niri/
│   │   └── mango/
│   └── server/     # JSON-RPC server over Unix Domain Socket
└── go.mod
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| CLI Commands | `cmd/axctl/main.go` | Argument parsing and RPC client logic |
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

## UNIQUE STYLES
- **Adapter Pattern**: Each compositor is a separate package implementing the `Compositor` interface.
- **State Caching**: Read-only queries are served from memory, updated by an event subscriber loop.

## COMMANDS
```bash
# Build
go build -o axctl ./cmd/axctl/main.go

# Start Daemon
./axctl daemon

# Query
./axctl window list
```

## NOTES
- **Hyprland**: Uses `$HYPRLAND_INSTANCE_SIGNATURE` for socket paths.
- **Niri**: Uses `$NIRI_SOCKET` for IPC.
- **Mango**: Uses `/run/user/$UID/mango.sock`.
