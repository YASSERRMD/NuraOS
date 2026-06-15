# Suite T11 — logging-time

Verifies that NuraOS emits well-structured, timestamped log output through the
serial console and exposes uptime metrics via Prometheus.

## Cases

| Case | Source | Pass condition |
|------|--------|----------------|
| `serial-log-timestamps` | Serial log file | ISO 8601 time pattern `T##:##:##` found |
| `log-levels-present` | Serial log / live buffer | `INFO`, `info`, or JSON `"level"` field present within 10 s |
| `metrics-uptime` | GET /metrics | Body contains `uptime` or `start_time` metric name |
| `structured-fields` | Serial log file | Logfmt `key=value` pairs or JSON `"level"` field present |

## Running

```
go run ./cmd/run-suite -- logging-time
```
