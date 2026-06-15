# NuraOS Init and Supervisor

## PID 1 strategy

NuraOS uses a two-stage PID 1 approach:

1. `/init` (shell script, exec'd by the kernel) -- sets up mounts, network,
   and /data, then `exec`s the supervisor.
2. `/sbin/supervisor` (shell script) -- becomes PID 1 and manages all services.

This keeps the early-boot environment configuration separate from service
lifecycle management. A Rust supervisor may replace the shell script in a
future phase for stricter resource control.

## /init responsibilities

| Step               | Detail                                              |
|--------------------|-----------------------------------------------------|
| Mount /proc        | Required for kernel interfaces                      |
| Mount /sys         | Device/driver info                                  |
| Mount /devtmpfs    | Automatic device nodes                              |
| Mount /tmp         | Ephemeral scratch space                             |
| Recovery check     | If `nura.recovery=1` is on cmdline, exec sh        |
| Bring up loopback  | `ip link set lo up`                                 |
| Bring up eth0      | udhcpc with /etc/udhcpc/default.script              |
| Write resolv.conf  | Fallback DNS if DHCP does not provide one           |
| Mount /data        | virtio-blk ext4; falls back to tmpfs if absent      |
| exec supervisor    | Hand off PID 1 control                              |

## /sbin/supervisor responsibilities

| Step                    | Detail                                           |
|-------------------------|--------------------------------------------------|
| Signal handling         | SIGTERM triggers clean shutdown of all services  |
| Stage 1: llama-server   | Start, health-poll on :8081/health               |
| Stage 2: nura-agent     | Start after llama-server, health-poll :7070/healthz |
| Stage 3: gateway        | Start after agent                                |
| Monitor                 | Restart crashed services with exponential backoff |
| Zombie reaping          | BusyBox sh at PID 1 calls waitpid automatically  |

## Boot stages

The supervisor writes the current stage to `/tmp/supervisor.stage` and to
`/data/logs/boot.log`. Stages in order:

```
stage1-llama-server
stage2-nura-agent
stage3-gateway
running
```

On shutdown:
```
shutdown
```

On recovery mode:
```
recovery
```

## Recovery mode

Append `nura.recovery=1` to the kernel command line:

```sh
# In run-qemu.sh KCMDLINE:
KCMDLINE="console=ttyS0,115200 panic=5 loglevel=7 nura.recovery=1"
```

`/init` detects the flag and drops to a BusyBox shell before the supervisor
starts. The shell has full access to all mounted filesystems.

## Service restart policy

Services are restarted on crash with exponential backoff:
- Initial delay: 1 second.
- Backoff factor: 2x per retry.
- Maximum delay: 30 seconds.
- After `MAX_RESTARTS` (5) consecutive failures, the delay is reset and the
  cycle repeats. There is no crash-loop breaker that permanently stops a service
  at this phase; permanent failure handling arrives in Phase 34.

## SIGTERM and clean shutdown

Sending SIGTERM to PID 1 (the supervisor) triggers:
1. SIGTERM forwarded to all managed services.
2. 5-second grace period for services to exit.
3. `sync` to flush filesystems.
4. `halt -f` to power off the VM.

This ensures provenance logs and session files are flushed before shutdown.

## Zombie reaping

BusyBox sh, when running as PID 1, calls `waitpid(-1, ...)` implicitly to
reap orphaned children. The supervisor's background service loops also
`wait $pid` explicitly. No explicit zombie-reaper loop is needed with BusyBox sh.
