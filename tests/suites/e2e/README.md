# Suite T16 - e2e

Full end-to-end system liveness check. Exercises every major gateway endpoint
in sequence. Intended as the final gate in a CI pipeline.

## Cases

| Case | Endpoint | Pass condition |
|------|----------|----------------|
| `healthz-ready` | GET /healthz | 200 (full system liveness) |
| `version-reachable` | GET /version | 200 with valid JSON body |
| `tools-reachable` | GET /tools | 200 |
| `models-reachable` | GET /models | 200 |
| `chat-model-required` | POST /chat | 200 (model loaded) or **skip** on 503 (model absent); 400 also accepted; fail only on unexpected 5xx |
| `telemetry-status` | GET /telemetry/status | 200 |

## Notes

`chat-model-required` never fails solely because a language model is not
present. The case is designed to confirm the gateway handles the missing-model
condition gracefully rather than panicking.

## Running

```
go run ./cmd/run-suite -- e2e
```
