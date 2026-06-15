# NuraOS Observability

## Logging

NuraOS uses the `tracing` crate for structured, leveled logging throughout the
agent core. Log events are written to two sinks simultaneously:

### Console sink

Human-readable compact format on stderr. Always active. Example:

```
2024-01-15T10:23:01Z  INFO nura_agent: nura-agent starting (nura-agent 0.1.0)
2024-01-15T10:23:01Z  INFO nura_agent: idle -- inference and REPL arrive in later phases
```

### File sink (/data/logs/agent.log)

JSON-lines format, active only when `/data/logs` is available (i.e., the `/data`
partition is mounted). Example line:

```json
{"timestamp":"2024-01-15T10:23:01Z","level":"INFO","target":"nura_agent","turn_id":"a1b2c3d4-...","message":"nura-agent starting"}
```

**Rotation:** when `agent.log` reaches 10 MB it is renamed to `agent.log.1`
and a new file is opened. One rotation is kept. Log files are never sent off-device.

### Log levels

| Level | Use case                                                 |
|-------|----------------------------------------------------------|
| ERROR | Unrecoverable error; process will exit or service restart |
| WARN  | Recoverable issue; degraded functionality possible        |
| INFO  | Normal operational events (start, turn begin/end, route) |
| DEBUG | Detailed flow for troubleshooting                         |
| TRACE | Per-token or per-frame data; high volume                  |

Default level: `info`. Override via `NURA_LOG_LEVEL` env var or `log_level` in
`agent.toml`. Fine-grained control via `RUST_LOG` (takes precedence).

## Correlation IDs

Every agent turn is tagged with a `TurnId` (UUID v4). Every HTTP request is
tagged with a `RequestId` (UUID v4). These IDs appear in every log event for
that scope, making cross-component tracing possible without a centralised
collector.

Example tracing span:

```
turn_id=a1b2c3d4-e5f6-... INFO starting turn
turn_id=a1b2c3d4-e5f6-... INFO routed to provider=local
turn_id=a1b2c3d4-e5f6-... INFO tool call: fs.read path=/data/etc/agent.toml
turn_id=a1b2c3d4-e5f6-... INFO turn complete tokens_in=128 tokens_out=64 elapsed_ms=3200
```

## Error model

All agent errors use the `NuraError` enum defined in `nura-core::error`.

| Variant         | Exit code | HTTP status | Meaning                              |
|-----------------|-----------|-------------|--------------------------------------|
| Config          | 2         | 500         | Config file missing or invalid        |
| Secrets         | 3         | 500         | Secrets file permission or parse err  |
| Provider        | 4         | 502         | Inference provider unreachable/error  |
| Tool            | 5         | 422         | Tool validation or execution failed   |
| BudgetExceeded  | 6         | 408         | Turn time or iteration limit hit      |
| Session         | 7         | 500         | Session store read/write failed       |
| Io              | 8         | 500         | I/O error                             |
| Internal        | 1         | 500         | Internal invariant violated           |

HTTP status codes are used by the Go gateway (Phase 28+). Exit codes are used
when the agent process terminates due to a fatal error.

## Metrics

Operational metrics (tokens/sec, turn latency, provider usage, uptime) are
exposed in Phase 31 via `/metrics` on the gateway. This document covers the
logging and error layers only.

## What is never logged

- API keys and gateway tokens (SecretString redacts in Debug/Display)
- Prompt or completion content at INFO level or above
- Personally identifiable information from tool call arguments

Use DEBUG or TRACE to see tool arguments during development. These levels must
not be enabled in production deployments.
