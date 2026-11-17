# Events and state

## Subscription lifecycle

Open a Unix connection to the axctl socket and send:

```json
{"id":1,"method":"System.Subscribe","params":{}}
```

The server returns `result: "subscribed"` and switches that connection to
streaming mode. The first event message is a `State.Dump`:

```json
{
  "jsonrpc": "2.0",
  "method": "State.Dump",
  "state": {
    "windows": [],
    "workspaces": [],
    "monitors": []
  }
}
```

After that, newline-delimited JSON notifications arrive with `jsonrpc: "2.0"`,
`method`, optional `params`, and, when related to compositor state, `state`.
There is no event history: the snapshot received on connection is the starting
point. Keep the connection open; close it to cancel the subscription. The same
connection can no longer be used for regular RPC requests.

`state` contains the normalized window, workspace, and monitor lists. Events on
the regular state-bearing path may also include `overview_open`; it is optional
and is not included in the initial `State.Dump`. This path includes compositor
events and the current idle-monitor and brightness events. Standalone
application, notification, and tray events do not include `state`.

## Compositor events

The `Event.*` names are notification methods sent by the daemon. `params`
contains the backend-adapted payload, so some legacy keys use uppercase names.
Do not rely on fields that are not listed in the table.

| Event method | Common parameters | When it is emitted |
|---|---|---|
| `Event.WindowCreated` | `Window` object | A new window appears. |
| `Event.WindowClosed` | `{ "ID": string }` | A window closes. |
| `Event.WindowFocused` | Backend payload; may include `id`, `address`, `title`, `appid`, or class. | Window focus changes. |
| `Event.WindowTitleChanged` | Backend payload (usually window ID/address and title). | The title changes. |
| `Event.WindowMoved` | Backend payload | A window moves to another workspace. |
| `Event.FloatingChanged` | Payload with `address` and `floating` | Floating state changes in a backend event that reports floating state. |
| `Event.WorkspaceChanged` | Backend-specific, often `{"Name": string}` or the original payload | Workspace/tag changes. |
| `Event.MonitorChanged` | Backend payload | A monitor is added, removed, or changed. |
| `Event.FocusedMonitorChanged` | Backend payload | The focused monitor changes. |
| `Event.FullscreenChanged` | Backend payload | Fullscreen changes; the cache is queried again. |
| `Event.OverviewChanged` | Payload with `is_open` when known | Overview state changes. |
| `Event.ConfigReloaded` | `params` may be omitted | The compositor config is reloaded. |
| `Event.CacheRefreshed` | `params` may be omitted | Axctl refreshes the cache after an event without a dedicated translation. |
| `Event.IdleMonitorChanged` | `{ "id": uint, "is_idle": bool }` | An RPC-created idle monitor changes state. |
| `Event.BrightnessChanged` | `{ "monitor": string, "value": number }` | Brightness is applied; `value` is the absolute normalized value from 0 to 1. |

For events that trigger a full cache refresh, the daemon refreshes before
broadcasting; handlers for targeted changes patch the cache before broadcasting.
The attached `state` is the cached snapshot at broadcast time, not an atomic
compositor snapshot, and failed backend queries can leave cached fields stale.
Query the list APIs again if you need to reconcile a UI with current state.

## Independent service events

These methods are emitted without a `state` snapshot:

| Method | `params` |
|---|---|
| `Event.ApplicationsChanged` | `{ "revision": uint }`; query `Application.List` or `Application.Search` again. |
| `Event.NotificationReceived` | Event with `type: "received"` and a `notification` object. |
| `Event.NotificationActionInvoked` | Event with a notification and `actionId`. |
| `Event.NotificationClosed` | Event with a notification and `reason`. |
| `Event.NotificationDeleted` | `deleted` event and the deleted ID. |
| `Event.NotificationHistoryCleared` | `history-cleared` event. |
| `Event.NotificationDNDChanged` | `dnd-changed` event; query `Notification.DND` for the value. |
| `Event.TrayChanged` | Event with `type: "changed"` and an `items` list. |
| `Event.TrayItemRemoved` | `removed` event with `id`. |

The notification object includes fields such as `id`, `appName`, `summary`,
`body`, `actions`, `urgency`, `time`, `timeout`, and `popup`; some fields are
optional. See the [IPC API](ipc-api.md) for more details about the methods.

## Ordering and slow consumers

Each subscriber has a serialized output queue to preserve message order. The
queue is finite (256 messages). If it fills because the consumer is not reading
quickly enough, axctl closes the connection instead of blocking the daemon or
silently dropping messages. The client must reconnect and receive a new
`State.Dump`.

Not all events have replay guarantees or a global ordering with independent
services. If a UI requires current state, query the state APIs after receiving
an event.
