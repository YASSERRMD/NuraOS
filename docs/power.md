# Power Management

NuraOS routes all power state transitions through `nura-manager` so that
services are always stopped in dependency-reverse order before the kernel
receives the halt or restart syscall.

## Power event sources

| Source | Mechanism | Manager signal |
|---|---|---|
| QEMU power button | ACPI power-button event -> kernel -> SIGPWR to PID 1 | `shutdownPoweroff` |
| `nuractl poweroff` | JSON command over `/run/nura-manager.sock` | `shutdownPoweroff` |
| `nuractl reboot` | JSON command over `/run/nura-manager.sock` | `shutdownReboot` |
| `SIGTERM` / `SIGINT` | OS signal to PID 1 | normal exit (no reboot syscall) |

All four sources converge on a single `shutdownCh` channel inside `Run()`.
The first event wins; subsequent events are silently dropped.

## Shutdown sequence

```
trigger received
  |
  v
cancel(ctx)          -- stops all service contexts immediately
  |
  v
ShutdownPlan()       -- reverse-dependency-order SIGTERM -> wait -> SIGKILL
  |  (max 30s total)
  v
jw.Close()           -- flush journal to disk
  |
  v
[if poweroff] syscall.Reboot(LINUX_REBOOT_CMD_POWER_OFF)
[if reboot]   syscall.Reboot(LINUX_REBOOT_CMD_RESTART)
[if normal]   return nil  -> supervisor exits -> kernel reaps PID 1
```

### Per-service grace period

Each service receives SIGTERM to its process group. If the process has not
exited after 15 seconds (`lifecycle.StopTimeout`), SIGKILL is sent to the
group. Services are stopped serially in reverse start order.

### Total shutdown timeout

If the full `ShutdownPlan` has not completed within 30 seconds
(`shutdownTotalTimeout`), the power action is forced immediately. A warning
is logged identifying the hung service in the journal.

## ACPI kernel configuration

```
CONFIG_ACPI=y
CONFIG_ACPI_BUTTON=y
```

`CONFIG_ACPI_BUTTON` registers a kernel driver that listens for ACPI
power-button and sleep-button events. On a power-button press (or a QEMU
`system_powerdown` command), the driver delivers `SIGPWR` to PID 1.

Because `nura-manager` runs as PID 1 (init is exec-replaced by the
supervisor, which exec-replaces itself with `nura-manager`), it receives
SIGPWR directly and initiates the shutdown sequence.

## nuractl usage

```sh
nuractl poweroff     # stop all services, then power off
nuractl reboot       # stop all services, then reboot
```

Both commands return immediately with `{"ok": true, "message": "..."}` once
the shutdown has been enqueued. The actual power action happens asynchronously
after `ShutdownPlan` completes.

## Journal flush guarantee

`jw.Close()` is called explicitly before any reboot syscall. Because
`syscall.Reboot()` does not return on success, deferred cleanup would never
run; the explicit close ensures no journal entries are lost.

After `jw.Close()` the logger falls back to writing to stdout only, which
remains visible on the serial console until the kernel shuts down devices.

## QEMU integration

To test poweroff from the QEMU monitor:

```
(qemu) system_powerdown
```

This sends an ACPI power-button event. The VM should stop all services, flush
the journal, and call `LINUX_REBOOT_CMD_POWER_OFF` within 30 seconds.

To test reboot:

```sh
# inside the VM
nuractl reboot
```

---

## CPU frequency profiles

NuraOS ships two CPU power profiles that control the `cpufreq` scaling governor
and the number of threads handed to llama-server.

### Profile definitions

| Profile | Governor | Threads | Use case |
|---|---|---|---|
| `performance` | `performance` | `nproc` (all cores) | Dedicated inference; lowest token-generation latency |
| `balanced` | `ondemand` | `nproc / 2` | Shared or battery-backed host; scales frequency on demand |

### Configuration

Edit `/etc/nura/cpu-profile.conf`:

```sh
# valid values: performance, balanced
profile=performance
```

Changes take effect on next boot. To apply immediately (as root):

```sh
nura-cpu-apply
```

### Boot-time application

`/init` calls `nura-cpu-apply` after virtual filesystems are mounted and before
any services start. The script:

1. Reads `/etc/nura/cpu-profile.conf`
2. Writes the governor string to every
   `/sys/devices/system/cpu/cpuN/cpufreq/scaling_governor` entry that exists
3. Writes the thread count to `/run/nura-cpu-threads`

If the kernel was built without `CONFIG_CPU_FREQ` or the guest has no
cpufreq driver (plain QEMU TCG without KVM P-states), the sysfs paths do not
exist and the governor writes are skipped silently. Thread count alignment still
works.

### Thread alignment

`/sbin/llama-server-wrapper` wraps the real `llama-server` binary. At launch it
reads `/run/nura-cpu-threads` (written by `nura-cpu-apply` at boot) and
prepends `--threads N` to the argument list:

```
performance profile, 4-core host: --threads 4
balanced    profile, 4-core host: --threads 2
```

The llama-server.toml unit file's `exec` field points to
`/sbin/llama-server-wrapper`; all other args (`--host`, `--port`, etc.) are
forwarded unchanged.

### Kernel configuration

```
CONFIG_CPU_FREQ=y
CONFIG_CPU_FREQ_STAT=y
CONFIG_CPU_FREQ_GOV_PERFORMANCE=y
CONFIG_CPU_FREQ_GOV_ONDEMAND=y
CONFIG_CPU_FREQ_DEFAULT_GOV_PERFORMANCE=y
CONFIG_X86_ACPI_CPUFREQ=y
CONFIG_CPU_IDLE=y
CONFIG_CPU_IDLE_GOV_LADDER=y
CONFIG_CPU_IDLE_GOV_MENU=y
```

`CONFIG_X86_ACPI_CPUFREQ` provides P-state frequency scaling in KVM guests via
ACPI. Without KVM (pure TCG emulation), the guest runs at a fixed emulated
frequency and governor changes are no-ops.

### Throughput measurement

Use `nura-bench-cpu` to record tokens/sec under each profile:

```sh
# Baseline (performance profile, current default):
nura-bench-cpu
# profile=performance threads=4 tokens=64 elapsed_ms=3200 tps=20

# Switch to balanced and measure:
sed -i 's/^profile=.*/profile=balanced/' /etc/nura/cpu-profile.conf
nura-cpu-apply
nura-bench-cpu
# profile=balanced threads=2 tokens=64 elapsed_ms=6100 tps=10
```

The script sends a fixed 64-token completion request to llama-server on
`127.0.0.1:8081` and reports tokens-per-second. Example results on a 4-core
KVM guest with Phi-3-mini-4k-instruct-q4 (representative; varies by hardware):

| Profile | Threads | Tokens/sec |
|---|---|---|
| performance | 4 | ~18-22 |
| balanced | 2 | ~9-12 |

The performance profile roughly doubles throughput on dedicated hardware;
balanced is preferred when the host has other workloads or thermal constraints.
