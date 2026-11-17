# Architecture and lifecycle

## Components

axctl is distributed as a single executable with two roles:

1. **Daemon** (`axctl daemon`): detects the compositor, initializes the cache,
   starts supporting services, and listens on a Unix socket.
2. **CLI** (`axctl <category> <action>`): connects to axctl's socket, sends an
   IPC method, and displays the result. `axctl subscribe` keeps a connection
   open to receive changes.

The common backend contract is defined by `pkg/ipc.Compositor`. Each backend
translates normalized operations to its native protocol: Hyprland sockets,
niri JSON-RPC, or Mango's mmsg protocol. axctl clients therefore do not need
to know about those transports.

## Compositor detection

The detection order depends on the session:

- If `MANGO_INSTANCE_SIGNATURE` is set or `XDG_CURRENT_DESKTOP` contains
  `mango`, axctl tries Mango first. If it cannot connect to the socket in this
  case, it does not continue trying the other compositors.
- In other sessions it tries Hyprland, then niri, and finally Mango.

Environment variables and socket discovery:

| Compositor | Detection and socket |
|---|---|
| Hyprland | `HYPRLAND_INSTANCE_SIGNATURE`; socket under `$XDG_RUNTIME_DIR/hypr/<signature>/`. |
| niri | `NIRI_SOCKET`; if unset, searches for `niri*.sock` in `$XDG_RUNTIME_DIR`. |
| Mango | `MANGO_INSTANCE_SIGNATURE` may be an absolute path or a name relative to `$XDG_RUNTIME_DIR`; if unset in a Mango session, axctl searches for active `mango-*.sock` sockets in `$XDG_RUNTIME_DIR` or `/run/user/<uid>`. |

`System.GetCompositor` returns `hyprland`, `niri`, `mango`, or `unknown`.
If no compositor is found, the daemon can still provide independent services
such as the application catalog and icon resolution. Methods that require a
compositor will return an error.

## axctl socket and authorization

The daemon socket path is selected in this order:

1. `AXCTL_SOCKET`, if set.
2. `$XDG_RUNTIME_DIR/axctl.sock`, if `XDG_RUNTIME_DIR` is non-empty.
3. `/tmp/axctl-<uid>.sock`, as a last resort.

The daemon and CLI must use the same effective path. On Linux, the server
checks peer credentials (`SO_PEERCRED`) and accepts only processes with the
same UID as the daemon. The socket is intended for local use by the same
account, not as a network API or a security boundary between mutually hostile
processes belonging to that user. `AXCTL_SOCKET` is also useful for tests or
isolated instances.

## Daemon startup

At startup, the daemon:

1. Detects the backend and checks whether another daemon is reachable at the
   configured path.
2. Initializes the server, cache, and supporting services.
3. If a compositor is available, reads the initial windows, workspaces, and
   monitors, then starts listening for compositor events.
4. If a compositor is available and the TOML file exists, loads it, generates
   the corresponding config, and watches the file and its existing imports for
   changes. Without a compositor, it skips TOML application and the config
   watcher.
5. Handles requests until it receives `SIGINT` or `SIGTERM`; on exit it closes
   services and removes the socket.

Configuration is optional. If no file exists, axctl does not generate or
apply an initial configuration. `-c <path>` selects the daemon's TOML file and
must appear before `daemon`, for example `axctl -c ./axctl.toml daemon`.

## Requests and data sources

A CLI request is converted to an object with `id`, `method`, and `params` and
sent to axctl's socket. The server validates the parameters, calls the
appropriate backend or service, and returns `result` or `error`. See the
[IPC API](ipc-api.md) for the full protocol.

The data source depends on the operation:

- `Window.List`, `Workspace.List`, and `Monitor.List` read from the in-memory
  cache.
- `Window.Active` and `Workspace.Active` query the backend at request time.
- The daemon populates the cache at startup and updates it from events. For
  events that may change several entities (for example, workspace, monitor, or
  fullscreen changes), it queries state again to avoid combining incompatible
  partial identifiers.
- `System.Subscribe` sends a `State.Dump` first, followed by events.
- Applications, icons, notifications, and tray data come from their respective
  services, not the window cache.

The cache is optimized for frequent queries. The server refreshes or patches it
before broadcasting the corresponding event where possible; the attached state
is the cache snapshot at broadcast time, not an atomic compositor snapshot, and
failed backend queries can leave cached fields stale. Consumers that need
current state should query the lists again.

## Errors and limitations

Backend errors are preserved as text in `error`. A feature unavailable on a
compositor usually returns `feature not supported`; this should not be
interpreted as a transport failure. Other common errors include a missing
daemon, a disconnected socket or compositor, invalid parameters, or an
unavailable D-Bus service.

IPC connections are local, but several methods execute system or compositor
commands. Only trusted processes belonging to the same user should have access
to the socket path.
