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

---

## Metrics

Operational metrics are exposed on the gateway in two forms:

- **`GET /metrics`** -- Prometheus text exposition (content-type
  `text/plain; version=0.0.4`). Compatible with `prometheus`, `victoria-metrics`,
  `grafana-agent`, and any OpenMetrics-aware scraper.
- **`GET /status`** -- human-readable JSON health summary.

Both endpoints respect bearer-token auth when `gateway_token` is set in
`/data/etc/secrets.toml`. They are subject to the same rate and concurrency
limits as other protected endpoints.

### Readiness signal

`GET /status` returns HTTP 200 when all components are healthy and HTTP 503 when
any component is degraded. The `overall` field distinguishes the two states:

```json
{
  "overall": "ok",
  "version": "v0.1.0",
  "uptime_seconds": 3600,
  "components": [
    {"name": "gateway", "status": "ok", "detail": "version v0.1.0"},
    {"name": "agent",   "status": "ok", "detail": "provider=local"}
  ]
}
```

When the agent socket is unreachable `overall` becomes `"degraded"` and the
agent component shows `"status": "degraded", "detail": "unreachable"`. A load
balancer or supervisor can use the HTTP status code directly as a health check.

### Prometheus metrics reference

All metric names use the `nura_gateway_` or `nura_agent_` prefix. Agent metrics
are fetched from the agent socket at scrape time; if the agent is unreachable
those families are omitted from the response (no stale zeros).

#### Gateway counters and gauges

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `nura_gateway_uptime_seconds` | gauge | -- | Seconds since the gateway process started |
| `nura_gateway_requests_total` | counter | `endpoint` | HTTP requests served per endpoint (`healthz`, `version`, `chat`, `tools`, `metrics`, `status`) |
| `nura_gateway_rate_limited_total` | counter | -- | Requests rejected by the per-IP rate limiter |
| `nura_gateway_concurrency_rejected_total` | counter | -- | Requests rejected by the global concurrency cap |
| `nura_gateway_chat_latency_microseconds_total` | counter | -- | Cumulative `/chat` handler latency (sum; divide by `chat_requests_completed_total` for mean) |
| `nura_gateway_chat_requests_completed_total` | counter | -- | `/chat` requests that received a complete response from the agent |
| `process_resident_memory_bytes` | gauge | -- | Memory obtained from the OS by the Go runtime |

#### Agent counters and gauges (proxied from the agent socket)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `nura_agent_uptime_seconds` | gauge | -- | Seconds since the nura-agent process started |
| `nura_agent_tokens_in_total` | counter | -- | Prompt tokens consumed across all turns |
| `nura_agent_tokens_out_total` | counter | -- | Completion tokens generated across all turns |
| `nura_agent_turns_total` | counter | -- | Completed inference turns |
| `nura_agent_tool_calls_total` | counter | `tool` | Tool invocations by tool name |
| `nura_agent_provider_requests_total` | counter | `provider` | Inference requests sent to each provider |

### Scraping configuration example (Prometheus)

```yaml
scrape_configs:
  - job_name: nura-gateway
    scheme: http
    authorization:
      type: Bearer
      credentials: <gateway_token>
    static_configs:
      - targets: ['127.0.0.1:8080']
    metrics_path: /metrics
    scrape_interval: 15s
```

### Deriving tokens/sec

```
rate(nura_agent_tokens_out_total[1m])
```

### Deriving mean /chat latency

```
nura_gateway_chat_latency_microseconds_total
  / nura_gateway_chat_requests_completed_total
```

---

## What is never logged or emitted in metrics

- API keys and gateway tokens (SecretString redacts in Debug/Display)
- Prompt or completion content at INFO level or above
- Personally identifiable information from tool call arguments
- Secret values in label names or metric names

Use DEBUG or TRACE to see tool arguments during development. These levels must
not be enabled in production deployments.
