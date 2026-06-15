# Suite T08 — services-http

Verifies HTTP API contract compliance for all major NuraOS gateway endpoints.

## Cases

| Case | Endpoint | Pass condition |
|------|----------|----------------|
| `healthz-contract` | GET /healthz | 200 JSON with `status` field |
| `version-fields` | GET /version | 200 JSON with `service` and `version` fields |
| `metrics-format` | GET /metrics | 200 body that starts with `#` (Prometheus text format) |
| `auth-required` | GET /chat | 401 when `auth_enabled: true` in /config; **skip** otherwise |
| `config-fields` | GET /config | 200 JSON with `gateway` and `agent` nested objects |
| `rate-limit-headers` | POST /chat (×5) | 429 or X-RateLimit-* headers; any non-5xx accepted if no limit triggered |
| `status-components` | GET /status | 200 JSON with `components` array or `agent` field |

## Prerequisites

- A booted NuraOS QEMU instance with the gateway reachable on `APIPort`.
- No language model required; all cases are deterministic in CI.

## Running

```
go run ./cmd/run-suite -- services-http
```
