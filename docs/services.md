# NuraOS Service Topology

NuraOS runs three services inside the QEMU guest, managed by `/sbin/supervisor`
(PID 1 after `/sbin/init` exec's it). Each service starts in a declared stage;
later stages wait for earlier ones to be healthy before proceeding.

## Boot stages

```
stage1-llama-server   llama-server (port 8081, loopback only)
stage2-nura-agent     nura-agent (Unix socket /run/nura-agent.sock)
stage3-gateway        nura-gateway (TCP port 8080, loopback by default)
```

## Services

### llama-server

| Item | Value |
|---|---|
| Binary | `/sbin/llama-server` |
| Listen | `127.0.0.1:8081` (HTTP) |
| Health | `GET http://127.0.0.1:8081/health` |
| Config | `/data/model.json` (path + context_length) |

llama.cpp inference server. The supervisor waits up to 120 s for the health
endpoint before starting nura-agent. If no model is found under `/data/models/`
the stage is skipped gracefully.

### nura-agent

| Item | Value |
|---|---|
| Binary | `/sbin/nura-agent` |
| User | `nura` (uid=1000) |
| IPC | Unix domain socket `/run/nura-agent.sock` |
| Protocol | JSON-over-HTTP (plain HTTP/1.1 on the socket) |

The Rust agent implements the AI turn loop. It binds the Unix socket before
accepting requests. The supervisor polls `/proc/net/unix` for the socket path
(30 s timeout) before allowing the gateway to start.

Agent endpoints on the socket:

| Method | Path | Description |
|---|---|---|
| GET | /health | `{"status":"ok","provider":"...","uptime_seconds":N}` |
| POST | /turns | Stream a conversation turn (SSE); body is TurnRequest |
| GET | /tools | List registered tools |
| GET | /metrics | Agent operational counters (JSON) |

### nura-gateway

| Item | Value |
|---|---|
| Binary | `/sbin/gateway` |
| User | `nura` (uid=1000) |
| Listen | `127.0.0.1:8080` default; `0.0.0.0:8080` with `GATEWAY_BIND_LAN=1` |
| Auth | Bearer token (optional; set `gateway_token` in secrets.toml) |
| Build | `scripts/build-gateway.sh` (static, `CGO_ENABLED=0`) |

The Go HTTP gateway translates external HTTP requests into IPC calls on the
agent socket. All endpoints (except `/healthz`) are protected by bearer auth
when a token is configured.

**Gateway HTTP endpoints:**

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | /healthz | Exempt | Agent reachability and status; 200 ok or 503 degraded |
| GET | /version | Required | `{"service":"nura-gateway","version":"..."}` |
| POST | /chat | Required | Chat turn (SSE); body `{"messages":[...],"provider":"..."}` |
| GET | /tools | Required | List tools from the agent |
| GET | /metrics | Required | Prometheus text with gateway and agent counters |
| GET | /status | Required | Human-readable JSON health summary across all components |
| GET | /config | Required | Effective gateway configuration (no secrets) |

**Middleware stack (outer to inner):**

```
securityHeadersMiddleware  -- X-Content-Type-Options, X-Frame-Options, CSP
bearerAuthMiddleware       -- 401 if token mismatch; /healthz exempt
rateLimitMiddleware        -- 429 per-IP at 1 RPS (burst 10); /healthz exempt
concurrencyMiddleware      -- 429 when 4+ concurrent non-health requests
mux                        -- route to handler
```

**POST /chat body:**

```json
{
  "messages": [
    {"role": "user", "content": "Hello"}
  ],
  "max_tokens": 1024,
  "temperature": 0.7,
  "provider": "local"
}
```

`provider` overrides the configured default for this turn only.
Valid values: `local`, `anthropic`, `openai`.

## QEMU port forwarding

```sh
qemu-system-x86_64 \
  -netdev user,id=n,hostfwd=tcp::18080-:8080 \
  -device virtio-net-pci,netdev=n \
  ...
```

Gateway is then reachable on the host at `http://localhost:18080`.

```sh
curl http://localhost:18080/healthz
curl http://localhost:18080/config
```

## IPC contract

The Go `services/internal/agent` package defines the shared types:

| Type | Description |
|---|---|
| `HealthResponse` | GET /health response |
| `TurnRequest` | POST /turns request body |
| `TurnEvent` | One SSE frame from POST /turns |
| `ToolsResponse` / `ToolInfo` | GET /tools response |
| `AgentMetrics` | GET /metrics response |
| `StatusResponse` / `StatusComponent` | GET /status response |

## Supervisor restart policy

All three services run under the supervisor's `start_service` loop:
- Crash restarts: up to 5 times with exponential backoff (1 s, 2 s, 4 s, 8 s, 16 s).
- After 5 crashes: 30 s cool-off, then counter resets.
- SIGTERM to supervisor: all services receive SIGTERM; supervisor waits 5 s then halts.

Services run as user `nura` (uid=1000) when `su` is available and the user
exists in `/etc/passwd`. Falls back to root with a warning if not.
