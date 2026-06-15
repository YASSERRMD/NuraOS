# NuraOS Architecture

This document describes the layered structure of NuraOS. Rendered diagrams will
be added as each layer is implemented. See the ADR directory for the reasoning
behind major decisions.

## Layers (top to bottom)

```
+---------------------------------------------------------+
|  User / External Client                                 |
|  (serial terminal or HTTP from host)                    |
+-----------------------------+---------------------------+
|  Go HTTP Gateway            |  Serial REPL              |
|  /chat, /tools, /healthz    |  (ttyS0, stdin/stdout)    |
+-----------------------------+---------------------------+
|              Rust Agent Core (nura-agent)               |
|  Turn manager, tool-call loop, context management,      |
|  system prompt, session store, provenance log           |
+-------------------+-------------------------------------+
|  Provider         |  Tool Layer                         |
|  Abstraction      |  (schema-validated, sandboxed,      |
|  (trait + router) |   allowlisted)                      |
+------+-----+------+-------------------------------------+
|Local |Anth-|OpenAI|  Local provider talks to            |
|llama |ropic|Compat|  llama-server on 127.0.0.1          |
|.cpp  |API  |EP    |                                     |
+------+-----+------+-------------------------------------+
|           Init Supervisor (PID 1)                       |
|  Starts and monitors: llama-server, nura-agent, gateway |
|  Signal forwarding, zombie reaping, recovery mode       |
+---------------------------------------------------------+
|           BusyBox Userland (static, musl)               |
|  sh, mount, ip, ps, halt -- minimal applet set          |
+---------------------------------------------------------+
|           Initramfs (cpio.gz, read-only in RAM)         |
|  Contains all the above; /data is the only writable FS  |
+---------------------------------------------------------+
|           Linux Kernel (kernel.org, tinyconfig base)    |
|  64-bit, virtio, ext4, serial console, TCP/IP           |
|  No loadable modules; everything built in               |
+---------------------------------------------------------+
|           QEMU x86-64 (primary target)                  |
|  virtio-blk /data, user-mode net, stdio serial          |
+---------------------------------------------------------+
```

## Storage model

| Mount    | Backing         | Mode       | Contents                        |
|----------|-----------------|------------|---------------------------------|
| `/`      | initramfs       | read-only  | all binaries, /init, BusyBox    |
| `/data`  | virtio-blk ext4 | read-write | models, logs, sessions, config  |
| `/proc`  | procfs          | virtual    | kernel process info             |
| `/sys`   | sysfs           | virtual    | kernel device/driver info       |
| `/dev`   | devtmpfs        | virtual    | device nodes                    |
| `/tmp`   | tmpfs           | ephemeral  | scratch space                   |

## Network model

QEMU user-mode networking. The guest gets an IP via udhcpc from the internal
QEMU DHCP server. Host-to-guest port forwards expose:

- HTTP API (gateway) on a configured port
- Metrics endpoint on a separate port

The guest can reach the internet for remote provider calls when configured.
Offline boot keeps all traffic local by default.

## Boot sequence

1. QEMU loads bzImage from host filesystem.
2. Kernel decompresses, detects virtio devices, mounts initramfs.
3. Kernel hands control to `/init` (shell script, PID 1 bootstrap).
4. `/init` mounts proc, sysfs, devtmpfs; brings up network.
5. `/init` mounts `/data` (falls back gracefully if absent).
6. `/init` exec's the supervisor.
7. Supervisor starts llama-server and waits for its healthcheck.
8. Supervisor starts nura-agent; agent waits for llama-server health.
9. Supervisor starts Go HTTP gateway.
10. Serial REPL and HTTP API become available.

## Key decisions

See [docs/adr/](adr/) for the full record. Summary:

- Raw kernel from kernel.org instead of Buildroot: maximum control, minimum
  surface area, no build system abstraction to debug.
- musl + BusyBox instead of glibc: fully static binaries, no dynamic linker.
- Rust for the agent core: memory safety, strong async support, static musl target.
- Go for networked services: fast compilation, good HTTP primitives.
- Provider trait in Rust: the agent loop is provider-agnostic; swapping inference
  backends needs no agent changes.
- llama.cpp HTTP backend (not FFI): debuggable, restartable independently,
  standard interface.
