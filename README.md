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

## Gateway API (v0.1)

Once the image is booted and the port is forwarded to the host:

```sh
# Health check (no auth needed)
curl http://localhost:18080/healthz

# View effective configuration
curl http://localhost:18080/config

# Chat (streaming SSE)
curl -X POST http://localhost:18080/chat \
  -H "Content-Type: application/json" \
  -d '{"messages":[{"role":"user","content":"Hello"}]}'

# List available tools
curl http://localhost:18080/tools

# Prometheus metrics
curl http://localhost:18080/metrics
```

Add `-H "Authorization: Bearer YOUR_TOKEN"` to authenticated endpoints when a
`gateway_token` is configured in `/data/etc/secrets.toml`.

## Provider configuration

Run the interactive setup helper to choose a provider and write secrets:

```sh
./scripts/configure.sh
```

Supported providers: `local` (llama.cpp on-device), `anthropic`, `openai`.
Provider can be overridden per turn with `"provider": "anthropic"` in the chat body.

## Repository conventions

- One branch per phase: `phase-NN-short-slug`. No direct pushes to `main`.
- Atomic conventional commits: `feat:`, `fix:`, `chore:`, `docs:`, `build:`,
  `test:`, `refactor:`, `ci:`, `perf:`.
- No em dash character anywhere (code, comments, docs, commits).
- Prefer "explore" or "investigate" over "experience" in prose.
- Secrets are never committed.

## License

MIT. See [LICENSE](LICENSE).
