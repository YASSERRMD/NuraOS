# NuraOS Service Model

NuraOS uses a declarative service manager (`nura-manager`) that reads TOML
unit files from `/etc/nura/services/`, resolves dependency order, and starts
services with readiness gating.

The shell supervisor at `/sbin/supervisor` is now a thin PID-1 wrapper that
delegates to `nura-manager`. It handles early boot, recovery mode, and provides
a legacy fallback path.

---

## Unit file format

Each service is a TOML file under `/etc/nura/services/<name>.toml`.

```toml
name        = "gateway"
description = "NuraOS HTTP gateway"
exec        = "/sbin/gateway"
args        = []              # optional extra arguments
type        = "longrun"       # oneshot | longrun | notify
user        = "nura"          # UNIX account; default root
after       = ["nura-agent"]  # ordering dependency (start after, no gate)
requires    = ["nura-agent"]  # hard dependency (wait for readiness)
enabled     = true

[restart]
policy             = "on-failure"  # no | on-failure | always
max_restarts       = 5
backoff_initial    = 1             # seconds
backoff_max        = 30
crash_loop_limit   = 5
crash_loop_window  = 60
crash_loop_backoff = 120

[resources]
cpu_weight = 100
memory_max = "128M"  # enforced in Phase 71 (cgroups)
io_weight  = 100

[readiness]
type    = "http"               # http | socket | none
url     = "http://127.0.0.1:8080/healthz"
socket  = ""
timeout = 30                   # seconds before continuing anyway
```

### Field reference

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Unique service identifier |
| `description` | string | no | Human-readable label |
| `exec` | string | yes | Absolute path to executable |
| `args` | []string | no | Extra arguments appended to exec |
| `type` | enum | no | `longrun` (default), `oneshot`, or `notify` |
| `user` | string | no | Drop to this UNIX account after launch |
| `after` | []string | no | Ordering constraint: start after these units |
| `requires` | []string | no | Hard dependency: wait for readiness before starting dependants |
| `enabled` | bool | no | `true` to include in the start plan (default false) |

### Restart policy

| Policy | Meaning |
|---|---|
| `no` | Never restart |
| `on-failure` | Restart only on non-zero exit |
| `always` | Restart unconditionally |

Crash-loop breaker: if the service crashes `crash_loop_limit` times within
`crash_loop_window` seconds, it is paused for `crash_loop_backoff` seconds
before retrying.

### Readiness probe types

| Type | Mechanism |
|---|---|
| `none` | No probe; dependants start immediately after process launch |
| `http` | HTTP GET to `url`; success = status < 500 |
| `socket` | Unix domain socket at `socket` path; success = connect OK |

### Socket activation

```toml
[socket_activation]
enabled      = true
network      = "tcp"      # "tcp" or "unix"
address      = "127.0.0.1:8080"
idle_timeout = 300        # seconds; 0 = no idle stop
```

When `enabled = true`:
1. The manager pre-opens the socket and binds `address` before the service starts.
2. The service is not started until the first client connection arrives.
3. The pre-opened socket fd is passed to the service as `LISTEN_FDS=1` (fd 3).
4. The service must detect `LISTEN_FDS=1` and call `net.FileListener(os.NewFile(3, ""))`.
5. If `idle_timeout > 0`, the manager stops the service after that many seconds of no
   connection activity. The next connection restarts it automatically.

Provenance and metrics counters survive stop/start cycles because they live in
the agent and provenance store, not in the gateway process.

The gateway (`/sbin/gateway`) supports socket activation natively: it checks
`LISTEN_FDS=1` at startup and uses the inherited fd if present.

---

## Dependency resolution

The service manager runs Kahn's topological sort over all enabled units. The
computed start order is stable and deterministic for a given set of units.

- `after` adds a graph edge (ordering only; no readiness gate).
- `requires` adds the same edge AND gates dependants behind the readiness probe.
- Cycles are detected and reported as errors; the manager refuses to start.

Print the computed plan without starting anything:

```sh
nura-manager dry-run
```

Validate unit files:

```sh
nura-manager check
```

---

## Services

### llama-server

| Item | Value |
|---|---|
| Unit file | `/etc/nura/services/llama-server.toml` |
| Binary | `/sbin/llama-server` |
| User | `root` |
| Listen | `127.0.0.1:8081` (HTTP) |
| Readiness | `GET http://127.0.0.1:8081/health` (timeout 120 s) |

llama.cpp inference server. The manager waits up to 120 s for the health
endpoint before starting nura-agent.

### nura-agent

| Item | Value |
|---|---|
| Unit file | `/etc/nura/services/nura-agent.toml` |
| Binary | `/sbin/nura-agent` |
| User | `nura` (uid=1000) |
| IPC | Unix domain socket `/run/nura-agent.sock` |
| Requires | `llama-server` |
| Readiness | socket probe on `/run/nura-agent.sock` (timeout 30 s) |

The Rust agent implements the AI turn loop. It binds the Unix socket before
accepting requests.

Agent socket endpoints:

| Method | Path | Description |
|---|---|---|
| GET | /health | `{"status":"ok","provider":"...","uptime_seconds":N}` |
| POST | /turns | Stream a conversation turn (SSE) |
| GET | /tools | List registered tools |
| GET | /metrics | Agent operational counters (JSON) |

### nura-gateway

| Item | Value |
|---|---|
| Unit file | `/etc/nura/services/gateway.toml` |
| Binary | `/sbin/gateway` |
| User | `nura` (uid=1000) |
| Listen | `127.0.0.1:8080` default; `0.0.0.0:8080` with `GATEWAY_BIND_LAN=1` |
| Requires | `nura-agent` |
| Readiness | `GET http://127.0.0.1:8080/healthz` (timeout 30 s) |

The Go HTTP gateway translates external HTTP requests into IPC calls on the
agent socket.

**Gateway HTTP endpoints:**

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | /healthz | Exempt | Agent reachability; 200 ok or 503 degraded |
| GET | /version | Required | `{"service":"nura-gateway","version":"..."}` |
| POST | /chat | Required | Chat turn (SSE) |
| GET | /tools | Required | Tool list from the agent |
| GET | /metrics | Required | Prometheus text |
| GET | /status | Required | Health summary JSON |
| GET | /config | Required | Effective gateway configuration |

**Middleware stack (outer to inner):**

```
securityHeadersMiddleware  -- X-Content-Type-Options, X-Frame-Options, CSP
bearerAuthMiddleware       -- 401 if token mismatch; /healthz exempt
rateLimitMiddleware        -- 429 per-IP at 1 RPS (burst 10); /healthz exempt
concurrencyMiddleware      -- 429 when 4+ concurrent non-health requests
mux                        -- route to handler
```

---

## Service lifecycle

The manager tracks each unit through the following states:

```
inactive -> starting -> ready -> running -> stopping -> failed
```

| State | Meaning |
|---|---|
| `inactive` | Not yet started |
| `starting` | Process launched, readiness probe pending |
| `ready` | Readiness probe passed; dependants may start |
| `running` | Process live, no active probe |
| `stopping` | SIGTERM sent, drain period active |
| `failed` | Exited non-zero and restart policy is `no` |

Detailed lifecycle transitions, notify protocol, and ordered shutdown are
implemented in Phase 57.

---

## Build

```sh
scripts/build-manager.sh     # builds rootfs/staging/sbin/nura-manager
scripts/build-initramfs.sh   # includes nura-manager and unit files in initramfs
```

The manager binary is built as a fully static Go binary (`CGO_ENABLED=0`).

---

## QEMU port forwarding

```sh
qemu-system-x86_64 \
  -netdev user,id=n,hostfwd=tcp::18080-:8080 \
  -device virtio-net-pci,netdev=n \
  ...
```

Gateway is reachable on the host at `http://localhost:18080`.

---

## IPC types (shared Go package)

The `services/internal/agent` package defines shared types used by the gateway:

| Type | Description |
|---|---|
| `HealthResponse` | GET /health response |
| `TurnRequest` | POST /turns request body |
| `TurnEvent` | One SSE frame from POST /turns |
| `ToolsResponse` | GET /tools response |
| `AgentMetrics` | GET /metrics response |
| `StatusResponse` | GET /status response |
