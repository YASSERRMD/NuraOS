# Suite T12 — devices-power

Verifies hardware detection, virtio-net connectivity, the board information
endpoint, and memory metrics exposure.

## Cases

| Case | Endpoint/Source | Pass condition |
|------|-----------------|----------------|
| `system-info-tool` | GET /tools | Response contains `system.info` tool name |
| `virtio-net-present` | GET /healthz | 200 (proves virtio-net host-forwarding works) |
| `board-endpoint` | GET /board | 200 with non-empty body |
| `metrics-memory` | GET /metrics | Body contains the word `memory` |

## Running

```
go run ./cmd/run-suite -- devices-power
```
