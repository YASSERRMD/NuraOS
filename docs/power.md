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
