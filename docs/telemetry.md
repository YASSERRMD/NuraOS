# NuraOS Telemetry

Telemetry in NuraOS is **off by default** and **opt-in only**. When enabled it
collects a small set of aggregate, non-PII counters and writes them locally.
Remote export requires an additional explicit opt-in.

## Privacy guarantees

The telemetry payload contains:

| Field | Description | PII? |
|---|---|---|
| `event` | Always `"heartbeat"` | No |
| `version` | Gateway binary version | No |
| `model` | Active model name from manifest | No |
| `uptime_seconds` | Seconds since gateway started | No |
| `turns_total` | Completed `/chat` responses | No |
| `timestamp` | UTC ISO-8601 timestamp | No |

What is explicitly **never** included:
- Prompt or completion text
- User identifiers or IP addresses
- File paths beyond the model name
- API keys or secrets

## Enabling telemetry

```sh
export NURA_TELEMETRY=1
```

The gateway writes a snapshot to `/data/etc/telemetry.json` on startup and
every 30 minutes. No network traffic is generated unless `NURA_TELEMETRY_URL`
is also set.

## Remote export (optional)

```sh
export NURA_TELEMETRY=1
export NURA_TELEMETRY_URL=https://telemetry.example.com/ingest
```

The gateway POSTs the JSON payload to the URL with
`Content-Type: application/json`. The local snapshot is always written first,
so you can inspect exactly what was sent.

## Local snapshot

```sh
cat /data/etc/telemetry.json
```

Example:

```json
{
  "event": "heartbeat",
  "version": "v0.1.0",
  "model": "smollm2-1.7b-instruct-q4_k_m",
  "uptime_seconds": 3600,
  "turns_total": 42,
  "timestamp": "2026-01-01T00:00:00Z"
}
```

Override the file path:

```sh
export NURA_TELEMETRY_FILE=/tmp/nura-telemetry.json
```

## Gateway endpoint

```
GET /telemetry/status
```

Reports the current configuration and the last written payload:

```json
{
  "telemetry": {
    "enabled": true,
    "remote_url": "https://telemetry.example.com/ingest",
    "local_file": "/data/etc/telemetry.json"
  },
  "last_payload": {
    "event": "heartbeat",
    "version": "v0.1.0",
    "model": "smollm2-1.7b-instruct-q4_k_m",
    "uptime_seconds": 3600,
    "turns_total": 42,
    "timestamp": "2026-01-01T00:00:00Z"
  }
}
```

`last_payload` is `null` until the first export interval fires or the gateway
restarts.

## Disabling telemetry

Telemetry is disabled by default. To confirm it is off:

```sh
unset NURA_TELEMETRY
# or
export NURA_TELEMETRY=0
```

`GET /telemetry/status` returns `"enabled": false` and the local file is not
written.
