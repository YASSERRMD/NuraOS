# NuraOS

A tiny, headless, AI-integrated operating system built from a raw Linux kernel
cloned from kernel.org with a hand-built minimal rootfs (no Buildroot). It boots
straight into an on-device AI agent reachable over serial console and a local
HTTP API.

## What it is

NuraOS is a purpose-built appliance OS whose sole job is to run an AI agent
locally on bare metal or a QEMU VM. There is no desktop, no package manager,
and no unnecessary services. The kernel is minimal, the userland is BusyBox, and
the agent is a statically compiled Rust binary.

Key properties:
- Local-first: the default boot path uses llama.cpp on the CPU. Remote providers
  (Anthropic, OpenAI-compatible) are opt-in.
- Minimal: every component is justified by the boot-to-agent path.
- Reproducible: all sources are pinned. Builds are locked and verifiable.
- Inspectable: every interaction is recorded in an append-only, hash-chained log.

## Build pipeline

```
kernel.org tarball (pinned, verified)
        |
        v
  bzImage (tinyconfig-based x86-64 config)
        |
   musl toolchain
        |
        v
  BusyBox (static)  +  nura-agent (Rust, static)  +  llama-server (CPU, static)
        |
        v
  initramfs (cpio.gz, busybox + agent + server + init script)
        |
  /data ext4 image (models, logs, sessions, config)
        |
        v
  QEMU x86-64 (serial console on stdio, virtio-blk /data, user-mode net)
        |
        v
  Serial REPL / HTTP API on forwarded port
```

## Directory layout

```
kernel/         Linux kernel source (fetched, not committed), config fragments, patches
rootfs/         Rootfs build scripts, skeleton tree, /init, tests
third_party/    Vendored sources: llama.cpp (pinned SHA)
agent/          Rust workspace: nura-agent (bin) + nura-core (lib)
services/       Go workspace: HTTP gateway and supporting services
models/         Placeholder for .gguf model files (gitignored, lives on /data)
image/          Image assembly scripts, partition layout, build outputs
scripts/        Build helpers: fetch-kernel, build-kernel, run-qemu, release
docs/           Architecture, ADRs, runbooks, operator guides
ci/             CI workflow sources
```

## Quick start

1. Check host prerequisites:
   ```
   ./scripts/check-host.sh
   ```
2. Fetch and verify the kernel source:
   ```
   ./scripts/fetch-kernel.sh
   ```
3. Build the full image:
   ```
   ./scripts/build-image.sh
   ```
4. Boot in QEMU:
   ```
   ./scripts/run-qemu.sh
   ```
5. Connect via the serial console (stdio) or HTTP on the forwarded port.

See [docs/host-setup.md](docs/host-setup.md) for the full host prerequisite list
and [docs/architecture.md](docs/architecture.md) for the system design.

## Gateway API (v1.0)

Once the image is booted and the port is forwarded to the host:

```sh
BASE=http://localhost:18080

# Health check
curl $BASE/healthz

# Chat -- streaming SSE
curl -X POST $BASE/chat \
  -H "Content-Type: application/json" \
  -d '{"messages":[{"role":"user","parts":[{"type":"text","text":"Hello"}]}]}'

# Effective configuration
curl $BASE/config

# Available tools
curl $BASE/tools

# Prometheus metrics
curl $BASE/metrics

# Health summary (all components)
curl $BASE/status

# Active model and installed models
curl $BASE/models

# A/B update slot state
curl $BASE/update/status

# Telemetry status (off by default)
curl $BASE/telemetry/status

# Hardware board info
curl $BASE/board
```

Add `-H "Authorization: Bearer YOUR_TOKEN"` to all endpoints when a
`gateway_token` is configured in `/data/etc/secrets.toml`.

### All endpoints

| Endpoint | Method | Auth? | Description |
|---|---|---|---|
| `/healthz` | GET | yes | Agent + gateway liveness |
| `/version` | GET | yes | Service and version string |
| `/chat` | POST | yes | Streaming SSE inference turn |
| `/tools` | GET | yes | List registered agent tools |
| `/metrics` | GET | yes | Prometheus text counters |
| `/status` | GET | yes | Human-readable health summary |
| `/config` | GET | yes | Effective gateway config (no secrets) |
| `/models` | GET | yes | Active model manifest + available GGUF list |
| `/update/status` | GET | yes | A/B slot and last update state |
| `/telemetry/status` | GET | yes | Telemetry enabled/disabled and last payload |
| `/board` | GET | yes | Hardware board info |

## Model management

```sh
# Download the default model
bash scripts/fetch-model.sh

# List installed models
bash scripts/model-list.sh

# Switch active model
bash scripts/model-activate.sh <model-name> --quantization Q4_K_M
```

## A/B safe update

```sh
# Stage a new rootfs to the inactive slot
bash scripts/update.sh --url https://example.com/nuraos.ext4 --sha256 <hex>

# Activate and reboot
bash scripts/slot-select.sh set b && reboot

# Roll back if the update is bad
bash scripts/update.sh --rollback && reboot
```

## Provider configuration

Run the interactive setup helper to choose a provider and write secrets:

```sh
./scripts/configure.sh
```

Supported providers: `local` (llama.cpp on-device), `anthropic`, `openai`,
`ollama` (via `NURA_OLLAMA=1`), `lm-studio` (via `NURA_LMSTUDIO=1`),
`custom` (via `NURA_CUSTOM_ENDPOINT=http://...`).

Provider can be overridden per turn with `"provider": "anthropic"` in the chat body.

## Smoke test

```sh
# Against a running gateway (default: localhost:8080)
bash scripts/smoke-test.sh

# Against a remote target
bash scripts/smoke-test.sh --base-url http://192.168.1.100:8080 --token mytoken
```

## Repository conventions

- One branch per phase: `phase-NN-short-slug`. No direct pushes to `main`.
- Atomic conventional commits: `feat:`, `fix:`, `chore:`, `docs:`, `build:`,
  `test:`, `refactor:`, `ci:`, `perf:`.
- No em dash character anywhere (code, comments, docs, commits).
- Prefer "explore" or "investigate" over "experience" in prose.
- Secrets are never committed.

## License

MIT. See [LICENSE](LICENSE).
