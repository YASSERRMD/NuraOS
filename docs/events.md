# System Event Bus

NuraOS ships a lightweight in-process pub/sub broker (`internal/eventbus`) that
carries system events between components and exposes them over a Unix socket so
external tools can observe the system without polling.

## Socket

```
/run/nura-events.sock   (mode 0644, any local process may connect)
```

## Protocol

Newline-delimited JSON. A client connects and sends one JSON line:

**Subscribe** -- receive all future events:
```json
{"subscribe":true}
```
The server streams events as JSON lines until the client disconnects.

**Publish** -- inject a single event:
```json
{"type":"custom.event","source":"my-tool","at":"2026-06-15T10:00:00Z","payload":{}}
```
The server broadcasts the event and closes the connection.

## Event schema

```json
{
  "type":    "<string>",
  "source":  "<string>",
  "at":      "<RFC3339 UTC>",
  "payload": { ... }
}
```

## Event taxonomy

### Service lifecycle (`source: "lifecycle"`)

| Type | Payload | When |
|---|---|---|
| `service.started` | `{"service":"<name>","pid":<n>}` | Service transitions to `running` |
| `service.stopped` | `{"service":"<name>"}` | Service stopped during ordered shutdown |
| `service.failed` | `{"service":"<name>","exit_code":<n>,"policy":"<p>"}` | Service exits and will not be restarted |

### Disk (`source: "diskmon"`)

| Type | Payload | When |
|---|---|---|
| `disk.warn` | `{"path":"/data","used_pct":<f>}` | Used space exceeds warn threshold (default 80%) |
| `disk.critical` | `{"path":"/data","used_pct":<f>}` | Used space exceeds critical threshold (default 95%) |

`disk.warn` also triggers automatic log/session reclaim; `disk.critical` starts
refusing new sessions.

### OOM (`source: "cgroup"`)

| Type | Payload | When |
|---|---|---|
| `oom.killed` | `{"service":"<name>","cgroup":"<path>"}` | Kernel OOM killer terminates a service process |

### Clock (`source: "timesync"`)

| Type | Payload | When |
|---|---|---|
| `clock.step` | `{"delta_ms":<n>}` | NTP step correction exceeds 1 second |

### Provider health (`source: "provider"`)

| Type | Payload | When |
|---|---|---|
| `provider.healthy` | `{"provider":"<name>"}` | Provider becomes reachable after a failure |
| `provider.degraded` | `{"provider":"<name>","error":"<msg>"}` | Provider health check fails |

## Backpressure

Each subscriber has a bounded channel (default 256 events). If the channel is
full when a publisher calls `Publish`, the event is **silently dropped for that
subscriber only**. Publishers never block; slow subscribers cannot stall system
components.

Components that must not miss events (e.g. audit logs) should subscribe early
and drain their channel faster than the publish rate, or increase the buffer
size passed to `bus.Subscribe(bufSize)`.

## nuractl usage

Tail all system events (Ctrl-C to stop):

```sh
nuractl events

# Example output:
# 2026-06-15T10:01:05Z  service.started          lifecycle {"service":"gateway","pid":42}
# 2026-06-15T10:01:07Z  service.started          lifecycle {"service":"nura-agent","pid":58}
# 2026-06-15T10:15:00Z  disk.warn                diskmon   {"path":"/data","used_pct":82.3}
```

JSON output:

```sh
nuractl events --json
```

## In-process subscription

Other Go services in the same process can subscribe without a socket:

```go
ch, cancel := bus.Subscribe(64)
defer cancel()
for ev := range ch {
    slog.Info("event", "type", ev.Type, "payload", ev.Payload)
}
```

## Publishing from external tools

```sh
# Inject a custom event from a shell script:
printf '{"type":"custom.alert","source":"my-script","at":"'$(date -u +%FT%TZ)'","payload":{"msg":"disk preheating complete"}}\n' \
  | nc -U /run/nura-events.sock
```
