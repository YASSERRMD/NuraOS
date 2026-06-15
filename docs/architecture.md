# NuraOS Architecture

NuraOS is a purpose-built headless appliance OS running an AI agent on bare
metal or inside QEMU. This document describes the component model, data flow,
and storage layout.

## System diagram

```
+---------------------------------------------------------------------+
|  Host machine (macOS / Linux)                                       |
|  QEMU x86-64 process                                                |
|                                                                     |
|  +---------------------------------------------------------------+  |
|  |  QEMU guest (512 MB RAM, 2 vCPUs)                            |  |
|  |                                                               |  |
|  |  [serial console / stdin-stdout]   [virtio-net + port-fwd]   |  |
|  |          |                                  |                 |  |
|  |          v                                  v                 |  |
|  |   [Serial REPL]           [Go HTTP Gateway 127.0.0.1:8080]   |  |
|  |   nura-agent repl         /healthz /version /chat /tools      |  |
|  |                           /metrics /status /config            |  |
|  |          |                          |                         |  |
|  |          +----------+---------------+                         |  |
|  |                     |                                         |  |
|  |                     v  (Unix socket /run/nura-agent.sock)     |  |
|  |          [Rust nura-agent]                                    |  |
|  |           Turn loop, tool-call loop, context window           |  |
|  |           Provider router, session store, provenance log      |  |
|  |                     |                                         |  |
|  |        +------------+------------+                            |  |
|  |        |                         |                            |  |
|  |        v (HTTP 127.0.0.1:8081)   v (HTTPS via SO_REUSEPORT)  |  |
|  | [llama-server]              [Anthropic/OpenAI API]            |  |
|  |  local model (gguf)          (opt-in, needs API key)          |  |
|  |                                                               |  |
|  |  Supervisor (PID 1, /sbin/supervisor)                        |  |
|  |  Init (/init), BusyBox userland                               |  |
|  |  Kernel (6.6.x, hardened, no modules)                        |  |
|  |                                                               |  |
|  |  /       -- initramfs (read-only, all binaries)              |  |
|  |  /data   -- virtio-blk ext4 (models, sessions, config)       |  |
|  +---------------------------------------------------------------+  |
|                                                                     |
+---------------------------------------------------------------------+
```

## Components

| Component | Language | Binary | Role |
|---|---|---|---|
| Linux kernel | C | bzImage | Hardware abstraction, scheduling, networking |
| BusyBox | C | `/bin/busybox` | Minimal POSIX userland (sh, mount, ip, ...) |
| init | sh | `/init` | Mounts filesystems, delegates to supervisor |
| supervisor | sh | `/sbin/supervisor` | PID 1 after init; stage ordering, crash restart |
| llama-server | C++ | `/sbin/llama-server` | CPU inference backend (llama.cpp) |
| nura-agent | Rust | `/sbin/nura-agent` | AI turn loop, tool execution, provider routing |
| nura-gateway | Go | `/sbin/gateway` | HTTP API, auth, rate limiting, SSE proxy |

## Data flow: POST /chat

```
Client HTTP POST /chat
        |
        v
Gateway securityHeadersMiddleware
        |
        v
Gateway bearerAuthMiddleware (401 if token mismatch)
        |
        v
Gateway rateLimitMiddleware (429 if per-IP quota exceeded)
        |
        v
Gateway concurrencyMiddleware (429 if semaphore full)
        |
        v
Gateway chat handler
  -- validates Content-Type, body <= 64 KiB --
        |
        v  POST /turns (JSON) over Unix socket
nura-agent turn handler
  -- selects provider, builds prompt, executes tool loop --
        |
        v  HTTP GET llama-server /completion
llama-server (blocking CPU inference)
        |
        v  SSE tokens streamed back
nura-agent SSE response
        |
        v  SSE proxy (4 KiB pool buffer)
Client receives text/event-stream
```

## Storage model

| Mount | Backing | Mode | Contents |
|---|---|---|---|
| `/` | initramfs (cpio.gz) | read-only | all binaries, /init, BusyBox |
| `/data` | virtio-blk ext4 | read-write | models, logs, sessions, config, secrets |
| `/proc` | procfs | virtual | kernel process info |
| `/sys` | sysfs | virtual | kernel device/driver info |
| `/dev` | devtmpfs | virtual | device nodes |
| `/run` | tmpfs | ephemeral | Unix sockets, PIDs |
| `/tmp` | tmpfs | ephemeral | scratch space |

`/data/etc/agent.toml` is the primary operator configuration point.
`/data/etc/secrets.toml` (mode 0600, owned by nura:nura) holds API keys.

## Network model

QEMU user-mode networking assigns the guest an IP via udhcpc. Host-to-guest
port forwards expose the gateway to the host:

```
Host:  curl http://localhost:18080/healthz
QEMU:  -netdev user,id=n,hostfwd=tcp::18080-:8080
Guest: gateway on 127.0.0.1:8080
```

`GATEWAY_BIND_LAN=1` widens the bind to `0.0.0.0` for LAN access.
A bearer token is required before enabling LAN bind.

## Boot sequence

1. QEMU loads `bzImage` from the host filesystem.
2. Kernel decompresses, probes virtio devices, mounts initramfs.
3. `/init` (sh, PID 1) mounts proc, sysfs, devtmpfs; brings up network.
4. `/init` mounts `/data` (gracefully skips if absent).
5. `/init` `exec`s `/sbin/supervisor`.
6. Supervisor: stage 1 -- starts `llama-server`, polls health (120 s timeout).
7. Supervisor: stage 2 -- starts `nura-agent` as user `nura`, polls socket.
8. Supervisor: stage 3 -- starts `nura-gateway` as user `nura`.
9. Serial REPL and HTTP API become available.

If a stage fails after 5 restarts the supervisor enters a 30 s cool-off then
resets the counter and retries indefinitely.

## Build pipeline

```
VERSIONS.env (pinned: kernel, musl, BusyBox, Rust, Go)
        |
        v
scripts/fetch-kernel.sh   scripts/fetch-musl.sh   scripts/fetch-busybox.sh
        |                         |                         |
        v                         v                         v
scripts/build-kernel.sh   scripts/cc-musl.sh       (musl toolchain used for all C)
        |
        v
bzImage + .config

scripts/build-agent.sh  (cargo build --target x86_64-unknown-linux-musl)
scripts/build-gateway.sh (CGO_ENABLED=0 go build)
scripts/build-llama.sh  (cmake + musl cc)
        |
        v
scripts/build-initramfs.sh  (cpio + gzip)
scripts/make-data-image.sh  (mkfs.ext4)
        |
        v
scripts/build-image.sh  (orchestrates all, writes manifest.json)
        |
        v
scripts/package-release.sh  -> dist/nuraos-<ver>.tar.gz + .sha256
```

## Key design decisions

See [docs/adr/](adr/) for rationale.

| Decision | Choice | Alternative considered |
|---|---|---|
| Kernel source | raw kernel.org tarball | Buildroot |
| C library | musl (static) | glibc |
| Userland | BusyBox | Buildroot minimal |
| Agent language | Rust | Python, Go |
| HTTP gateway | Go | Rust (same binary) |
| Inference backend | llama.cpp HTTP server | FFI into llama.cpp |
| Provider interface | Rust trait | config-driven dispatcher |
| IPC | Unix socket, HTTP/1.1 | D-Bus, gRPC |
| Session log | append-only JSONL + SHA-256 chain | SQLite |

## Threat model summary

| Threat | Mitigation |
|---|---|
| Unauthenticated LAN access | Bearer token; loopback-only default |
| Request flooding | Per-IP rate limiter (1 RPS, burst 10) |
| Runaway concurrent requests | Global concurrency cap (4 slots) |
| MIME confusion / clickjacking | Security headers middleware |
| Key leakage via logs | Secrets never logged; /config omits them |
| Privilege escalation | nura service account (uid=1000); no sudo |
| Kernel exploit | KASLR, W^X, stack protector, SECCOMP |

See [docs/security.md](security.md) for the full treatment.
