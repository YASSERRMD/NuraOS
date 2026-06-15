# Suite T15 — performance

Measures endpoint latency and artifact size budgets to catch regressions in
NuraOS responsiveness and image bloat.

## Cases

| Case | Threshold | Fail condition |
|------|-----------|----------------|
| `healthz-rtt` | 500 ms | GET /healthz RTT exceeds threshold |
| `metrics-rtt` | 1000 ms | GET /metrics RTT exceeds threshold |
| `boot-image-size` | bzImage < 100 MiB, initramfs < 50 MiB | Either artifact over budget (missing artifacts are ignored) |
| `serial-boot-ready` | 200 ms | Pre-booted instance /healthz RTT exceeds threshold |

## Notes

All latency measurements use wall-clock time from just before the HTTP request
to just after the response body is fully read. QEMU TCG emulation adds overhead;
thresholds are calibrated for emulated environments.

## Running

```
go run ./cmd/run-suite -- performance
```
