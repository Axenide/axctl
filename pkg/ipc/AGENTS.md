# IPC ADAPTERS

## OVERVIEW
This package contains the core abstraction layer for Wayland compositors. Each subdirectory represents a specific compositor's IPC implementation.

## STRUCTURE
```
pkg/ipc/
├── hyprland/    # Socket2 + Dispatcher implementation
├── niri/        # JSON-RPC request/reply implementation
├── mango/       # Text-based socket implementation
├── mock/        # Test utilities and interface mocks
├── interface.go # The unified Compositor interface
└── types.go     # Shared domain models (Window, Workspace, Event)
```

## WHERE TO LOOK
- **Interface Definition**: `interface.go` is the source of truth for all supported actions.
- **Shared Types**: `types.go` defines the cross-compositor schema for windows and workspaces.
- **Testing**: `mock/compositor.go` should be used for all unit tests requiring IPC.

## CONVENTIONS
- **ID Normalization**: Convert all compositor-specific IDs to normalized strings.
- **Event Mapping**: Compositor-specific events MUST be mapped to the generic `Event` types in `types.go`.
- **Active Window**: Use `ActiveWindow()` method to resolve targets when ID is omitted in CLI calls.

## ANTI-PATTERNS
- **Package Leaks**: Do not expose compositor-specific types outside of their respective packages.
- **Blocking Calls**: Ensure event subscriptions do not block the main command execution path.
- **Direct Net Dial**: Always use the provided abstraction rather than manual socket dialing.
