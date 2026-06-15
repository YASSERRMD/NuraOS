# NuraOS Service Isolation Model

NuraOS isolates services at two layers: per-service UNIX accounts (Phase 68) and
Linux namespace cloning (Phase 69). This document covers the namespace layer.

## Why namespaces

Linux namespaces limit what a compromised service can observe or affect:

| Namespace | Flag            | Protects against                                     |
|-----------|-----------------|------------------------------------------------------|
| IPC       | CLONE_NEWIPC    | Cross-service shared memory, message queues, semaphores |
| UTS       | CLONE_NEWUTS    | Hostname/domainname spoofing visible to other processes |
| Mount     | CLONE_NEWNS     | Rogue bind/pivot that would pollute the parent mount table |
| PID       | CLONE_NEWPID    | Ptrace/signal attacks against processes in the host PID tree |
| Network   | CLONE_NEWNET    | Sniffing or rebinding network interfaces and ports   |

## Kernel configuration

Namespace support is compiled in via `kernel/configs/nuraos_x86_64_defconfig`:

```
CONFIG_NAMESPACES=y
CONFIG_IPC_NS=y
CONFIG_UTS_NS=y
CONFIG_PID_NS=y
CONFIG_NET_NS=y
CONFIG_USER_NS=n   # disabled: no container use case, reduces attack surface
```

## Per-unit configuration

Each service declares its namespace set in the `[namespaces]` section of its TOML
unit file. All fields default to `false`; enable selectively:

```toml
[namespaces]
pid     = false   # new PID namespace (requires subreaper care)
mount   = false   # private mount table; inherits parent view on entry
ipc     = false   # private IPC namespace
uts     = false   # private hostname/domainname view
network = false   # private network stack (loopback only unless configured)
```

### Current per-service assignments

| Service       | pid | mount | ipc | uts | network | Rationale                                      |
|---------------|-----|-------|-----|-----|---------|------------------------------------------------|
| llama-server  | no  | yes   | yes | yes | no      | Most isolated; no network isolation needed as it binds to 127.0.0.1:8081 for inter-service calls |
| nura-agent    | no  | no    | yes | no  | no      | IPC isolation; needs host network and filesystem access |
| gateway       | no  | no    | yes | no  | no      | IPC isolation; inherits socket-activated fd from manager |

Network isolation (`network = true`) is not enabled for any service because all
inter-service communication uses loopback TCP. A new network namespace would
start with only a loopback interface and break connectivity without explicit
veth/bridge setup.

PID isolation (`pid = true`) is reserved for future phases; the new namespace
would need a proper init (PID 1) inside it or the kernel orphan-reaping behavior
must be considered.

## Mount namespace behaviour

When `mount = true` the kernel clones the parent mount table into a fresh private
namespace before execing the child. The service starts with the same filesystem
view as the manager. Any subsequent `mount(2)` calls inside the service do not
propagate back to the manager or to other services. This prevents a rogue binary
from mounting a malicious overlay that other services would see.

A future phase can refine the mount view further by performing bind mounts inside
the new namespace before dropping privileges, giving each service only the
subdirectories of `/data` it legitimately needs.

## IPC namespace behaviour

Each service with `ipc = true` runs in a fresh IPC namespace. It cannot attach
to or destroy POSIX message queues, System V semaphores, or shared memory
segments created by other services or the manager. This is the safest namespace
to enable unconditionally because services do not rely on cross-service IPC.

## Inter-service connectivity

Namespaces do not affect UNIX sockets or TCP connections across namespace
boundaries (only `network` namespaces affect that, and it is disabled). The
following paths work as before:

| Path                       | Used by             |
|----------------------------|---------------------|
| `/run/nura-agent.sock`     | gateway -> agent    |
| `127.0.0.1:8081`           | agent -> llama-server |
| `127.0.0.1:8080` (inherited fd) | clients -> gateway |

## Implementation

The service manager (`services/internal/lifecycle/`) applies namespaces at spawn
time via build-tagged helpers:

- `ns_linux.go` -- sets `syscall.SysProcAttr.Cloneflags` from `unit.Namespaces`
- `ns_other.go` -- no-op stub for non-Linux (Darwin dev builds)

Both `spawnProcess` (longrun/oneshot) and the socket-activation goroutine call
`applyNamespaces` before `cmd.Start()`.
