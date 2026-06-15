# NuraOS Logging Subsystem

NuraOS captures all service output and kernel messages in a structured,
day-partitioned journal stored under `/data/journal`.

## Journal format

Each file is named `YYYY-MM-DD.journal` and contains newline-delimited JSON
(NDJSON). One record per line:

```json
{"ts":"2025-01-15T12:34:56.789Z","svc":"gateway","pid":1234,"pri":6,"msg":"listening on :8080"}
```

| Field | Type   | Description |
|-------|--------|-------------|
| `ts`  | string | RFC 3339 UTC timestamp |
| `svc` | string | Service name (or `kernel` for kmsg) |
| `pid` | int    | Process ID (omitted when zero) |
| `pri` | int    | Syslog priority (RFC 5424): 0=emergency ... 7=debug |
| `msg` | string | Log message text |

## Priority levels

| Value | Name      |
|-------|-----------|
| 0     | emergency |
| 1     | alert     |
| 2     | critical  |
| 3     | error     |
| 4     | warning   |
| 5     | notice    |
| 6     | info      |
| 7     | debug     |

Service stdout is recorded at `info` (6); stderr at `error` (3). Kernel
messages from `/dev/kmsg` are recorded under the `kernel` service name with
the priority extracted from the syslog prefix.

## Size cap and rotation

The writer enforces a configurable total size cap (default 100 MiB) across
all day files. When the cap is exceeded the oldest day files are deleted
until total usage is within the cap. Rotation to a new day file happens
automatically at midnight UTC.

## Querying logs with nuractl

```
# last 50 lines for a service
nuractl logs gateway

# last N lines
nuractl logs gateway -n 100

# JSON output
nuractl logs gateway --json
```

## Direct journal access

The `journal` package exposes three query functions:

```go
// Return records matching filter (chronological order).
recs, err := journal.Query(dir, journal.Filter{
    Service:     "gateway",
    MinPriority: journal.PriInfo,
    Since:       time.Now().Add(-1 * time.Hour),
    Limit:       200,
})

// Return the last N records matching filter.
tail, err := journal.Tail(dir, 50, filter)

// Stream new records as they are written.
stopCh := make(chan struct{})
journal.Follow(dir, filter, stopCh, func(r journal.Record) {
    fmt.Println(r.Message)
})
```

## Architecture

```
nura-manager
  |-- spawnProcess()
  |     |-- cmd.StdoutPipe() --> journal.Collect(..., PriInfo)
  |     `-- cmd.StderrPipe() --> journal.Collect(..., PriError)
  |
  |-- socketActivate()
  |     |-- cmd.StdoutPipe() --> journal.Collect(..., PriInfo)
  |     `-- cmd.StderrPipe() --> journal.Collect(..., PriError)
  |
  `-- CollectKmsg("/dev/kmsg") --> kernel records
```

`nura-manager` opens the journal at `/data/journal` on startup. If the
directory cannot be created (e.g. read-only rootfs in early boot) the manager
falls back to writing service output directly to its own stdout with a warning.

## Severity routing

The `journal.NewRouter` function returns a `slog.Handler` that implements
severity-based routing:

| Destination | Minimum priority |
|-------------|-----------------|
| Journal     | debug (all)      |
| Console     | warning          |

This means the console only shows actionable messages while the journal retains
the full debug trace for post-mortem analysis.

```go
handler := journal.NewRouter(jw, os.Stdout, "nura-manager")
log := slog.New(handler)
```

When the journal is unavailable (`jw == nil`), the router falls back to a
plain text handler at Info level so no messages are lost.

## Flood rate limiting

Services that emit more than `defaultLogRatePerSec` (200) lines per second
have the excess silently dropped at the `Writer` level. The per-service
limit is implemented via a sliding one-second token bucket.

```go
limiter := journal.NewFloodLimiter(200)
jw.SetLimiter(limiter)
```

Records from other services are unaffected; each service has its own bucket.

## Secret redaction

The `Redact` function masks common secret patterns before records leave the
system (used by the forwarder). Patterns cover:

- `password=`, `secret=`, `token=`, `api_key=`, `apikey=`, `auth_key=`,
  `credential=` assignments (case-insensitive)
- `Bearer <token>` in Authorization headers

The key name is preserved; only the value is replaced with `[REDACTED]`.

```go
redacted := journal.Redact(msg, journal.DefaultRedactPatterns)
```

Custom patterns can be substituted via `ForwardConfig.RedactPatterns`.

## Remote forwarding

Log forwarding is opt-in and disabled by default. Enable it by setting the
`NURA_FORWARD_URL` environment variable before starting `nura-manager`:

```
# UDP syslog (RFC 5424) to a remote collector
NURA_FORWARD_URL=udp://syslog.internal:514

# HTTP/HTTPS JSON POST to an aggregator or OTLP-compatible receiver
NURA_FORWARD_URL=https://logs.example.com/ingest
```

Only Warning-and-above records are forwarded. Records are redacted with
`DefaultRedactPatterns` before transmission.

### Kill switch

Forwarding can be disabled at runtime by either:

1. Creating the file `/data/journal/no-forward` on the running system.
   The forwarder detects it on the next record and stops permanently.

2. Calling `(*Forwarder).Kill()` programmatically.

Neither method restarts forwarding until the process is restarted with a new
`Forwarder` instance.
