# NuraOS Service Topology

NuraOS runs three services inside the QEMU guest, managed by `/sbin/supervisor`
(PID 1 after `/sbin/init` exec's it). Each service starts in a declared stage;
later stages wait for earlier ones to be healthy before proceeding.

## Boot stages

```
stage1-llama-server   llama-server (port 8081, loopback only)
stage2-nura-agent     nura-agent (Unix socket /run/nura-agent.sock)
stage3-gateway        nura-gateway (TCP port 8080, all interfaces)
```

## Services

### llama-server

| Item | Value |
|------|-------|
| Binary | `/sbin/llama-server` |
| Listen | `127.0.0.1:8081` (HTTP) |
| Health | `GET http://127.0.0.1:8081/health` |
| Config | `/data/model.json` (path + context_length) |

llama.cpp inference server. The supervisor waits up to 120 s for the health
endpoint before starting nura-agent. If no model is found under `/data/models/`
the stage is skipped gracefully.

### nura-agent

| Item | Value |
|------|-------|
| Binary | `/sbin/nura-agent` |
| IPC | Unix domain socket `/run/nura-agent.sock` |
| Protocol | JSON-over-HTTP (plain HTTP/1.1 on the socket) |

The Rust agent implements the AI turn loop. It binds the Unix socket before
accepting requests. The supervisor polls `/proc/net/unix` for the socket path
(30 s timeout) before allowing the gateway to start.

Endpoints exposed on the socket (Phase 28 stubs; fully implemented in Phase 29+):

| Method | Path | Description |
|--------|------|-------------|
| GET | /health | Returns `{"status":"ok","provider":"...","uptime_seconds":N}` |
| POST | /turns | Stream a conversation turn (SSE) |
| GET | /tools | List registered tools |

### nura-gateway

| Item | Value |
|------|-------|
| Binary | `/sbin/gateway` |
| Listen | `0.0.0.0:8080` (TCP) |
| Env | `GATEWAY_PORT` overrides 8080 |
| Build | `scripts/build-gateway.sh` (static musl, CGO_ENABLED=0) |

The Go HTTP gateway translates external HTTP requests into IPC calls on the
agent socket. Phase 28 ships `/healthz` and `/version`; Phase 29 adds `/chat`
(SSE streaming) and `/tools`.

Endpoints (Phase 28):

| Method | Path | Description |
|--------|------|-------------|
| GET | /healthz | `{"status":"ok","agent_reachable":true}` or 503 degraded |
| GET | /version | `{"service":"nura-gateway","version":"0.1.0"}` |

## QEMU port forwarding

The recommended QEMU invocation forwards guest port 8080 to the host. Add
`-netdev user,id=net0,hostfwd=tcp::18080-:8080` and `-device virtio-net-pci,netdev=net0`
to the QEMU command so the gateway is reachable on the host at `http://localhost:18080`.

Example:

```sh
curl http://localhost:18080/healthz
curl http://localhost:18080/version
```

## IPC contract

The Go `services/internal/agent` package defines the shared types used by the
gateway when calling the agent socket. Phase 28 defines the structs; Phase 29
implements the HTTP client.

```
services/
  cmd/gateway/          -- gateway binary (main package)
  internal/agent/       -- IPC contract types (SocketPath, TurnRequest, ...)
```

## Supervisor restart policy

All three services run under the supervisor's `start_service` loop:
- Crash restarts: up to 5 times with exponential backoff (1 s, 2 s, 4 s, 8 s, 16 s).
- After 5 crashes: 30 s cool-off, then counter resets.
- SIGTERM to supervisor: all services receive SIGTERM; supervisor waits 5 s then halts.
