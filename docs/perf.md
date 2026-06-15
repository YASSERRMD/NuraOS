# Performance Profiling

NuraOS ships benchmarks and an optional pprof endpoint to profile the gateway
and agent under real workloads.

## Benchmark suite

Run the micro-benchmarks from the repository root:

```
cd services
go test ./cmd/gateway/ -run='^$' -bench=. -benchmem
```

Key results on Apple M4 (arm64, 2026-06):

| Benchmark | Iterations | ns/op | Notes |
|---|---|---|---|
| BenchmarkChatHandler | 248 k | 23 560 | Full SSE round-trip via Unix socket |
| BenchmarkMetricsWriteTo | 51 k | 23 942 | Prometheus text with agent metrics |
| BenchmarkRateLimiter | 28 M | 43 | Per-IP token-bucket acquire |
| BenchmarkRateLimiterParallel | varies | varies | 10 goroutines competing |
| BenchmarkSecurityHeaders | 1.3 M | 895 | Three header writes per request |
| BenchmarkConcurrencyMiddleware | 1.9 M | 638 | Channel send + deferred recv |

The dominant cost for `/chat` is Unix socket latency, not gateway logic.
Gateway-only middleware adds roughly 1.5 us per request.

## pprof CPU and heap profiling

Set `NURA_PPROF=1` before starting the gateway to enable a loopback-only
profiling server on `127.0.0.1:6060`:

```
NURA_PPROF=1 ./nura-gateway
```

This exposes the standard `net/http/pprof` endpoints:

| Path | Purpose |
|---|---|
| `/debug/pprof/` | Index page |
| `/debug/pprof/profile?seconds=30` | 30-second CPU profile |
| `/debug/pprof/heap` | Heap snapshot |
| `/debug/pprof/goroutine` | Goroutine stacks |
| `/debug/pprof/trace?seconds=5` | Execution trace |

The pprof server binds only to `127.0.0.1` and requires no auth token.
Never expose it to the network.

Capture a 30-second CPU profile while running a load test:

```
go tool pprof -http=:8081 http://127.0.0.1:6060/debug/pprof/profile?seconds=30
```

Capture a heap profile:

```
go tool pprof -http=:8081 http://127.0.0.1:6060/debug/pprof/heap
```

## Chat SSE buffer pool

The `/chat` SSE proxy reuses 4 KiB read buffers via `sync.Pool` instead of
allocating one per request. Under sustained streaming load this eliminates
steady-state GC pressure from short-lived byte slices.

## Tuning knobs

| Env variable | Default | Effect |
|---|---|---|
| `GATEWAY_PORT` | `8080` | Listening port |
| `GATEWAY_BIND_LAN` | unset | Set to `1` to accept LAN connections |
| `NURA_PPROF` | unset | Set to `1` to start the pprof server |

Rate-limit and concurrency constants in `ratelimit.go`:

| Constant | Value | Effect |
|---|---|---|
| `defaultRPS` | 1.0 | Requests per second per client IP |
| `defaultBurst` | 10 | Token-bucket burst headroom |
| `maxConcurrent` | 4 | Simultaneous non-health requests |

These are intentionally conservative for an embedded appliance. Increase
`maxConcurrent` if the host has more CPU cores dedicated to the gateway.

## Execution trace

For latency distribution analysis use the execution tracer:

```
curl "http://127.0.0.1:6060/debug/pprof/trace?seconds=5" -o trace.out
go tool trace trace.out
```

This surfaces goroutine scheduling latency, GC stop-the-world pauses, and
syscall blocking in the Unix socket path.
