# NuraOS Resilience

This document describes the fault-tolerance mechanisms that keep NuraOS running
(or recovering gracefully) after hardware failures, software hangs, and
unexpected crashes.

---

## Watchdog subsystem

NuraOS uses a two-tier watchdog to recover from total hangs where even the
service manager becomes unresponsive.

### Tier 1 - hardware watchdog

The kernel opens `/dev/watchdog` early in boot. A background goroutine (the
_petter_) writes a keep-alive byte every `PetInterval` (default 10 s). If the
petter fails to write within the hardware watchdog timeout (default 30 s), the
kernel triggers a hard reset -- even if the OS is completely hung.

On QEMU, expose the virtual i6300ESB watchdog:

```
-device i6300esb,id=wdog0
```

On real hardware, any device that exposes `/dev/watchdog` with the standard
Linux watchdog API (WDIOF_SETTIMEOUT ioctl) is supported.

### Tier 2 - software watchdog / escalation ladder

A second goroutine (the _supervisor_) polls a user-supplied `HealthFunc` every
`SoftwareInterval` (default 5 s). If `HealthFunc` returns false
`SoftTries` consecutive times (default 3), the supervisor _escalates_: it
stops the petter goroutine. Without further keep-alive writes, the hardware
watchdog expires within `HardwareTimeout` and the kernel resets the system.

Escalation ladder:

```
HealthFunc fails
    -> 1st failure  -> log warning
    -> 2nd failure  -> log warning
    -> 3rd failure  -> supervisor exits, petter stops
        -> hardware watchdog expires (~30 s)
            -> kernel hard reset
```

### Configuration

```go
watchdog.New(watchdog.Config{
    DevPath:          "/dev/watchdog",   // hardware device
    PetInterval:      10 * time.Second,  // keep-alive frequency
    SoftwareInterval: 5 * time.Second,   // health poll frequency
    SoftTries:        3,                 // failures before escalation
    HealthFunc:       myHealthCheck,
    Log:              slog.Default(),
})
```

### Clean shutdown

On a planned shutdown, call `w.Close()` which writes the magic disarm byte
`"V"` to `/dev/watchdog` before closing the file descriptor. The kernel
recognises this and disarms the watchdog so the system shuts down cleanly
without a spurious reset.

### Integration with the service manager

The NuraOS service manager should call `Start(ctx)` after opening all service
sockets. The `HealthFunc` checks:

1. The control socket is responsive (round-trip to self).
2. All essential services (gateway, agent) are in the `running` state.
3. Entropy pool has at least 64 bits (via `selftest.NewBootRunner`).

If any of these fail three times in a row, escalation reboots the system.

### Testing

The `watchdog` package ships unit tests that verify:

- A healthy system never triggers escalation.
- Consecutive failures escalate after `SoftTries`.
- `Pet` and `Close` are safe when no hardware device is present.
- `StopPetting` is idempotent.

To simulate a hang in integration tests, set `HealthFunc` to always return
false and assert the supervisor exits within `SoftTries * SoftwareInterval`.

---

## Circuit breaker (provider health)

AI provider endpoints are guarded by a three-state circuit breaker
(Closed / Open / HalfOpen). See [providers.md](providers.md) for the full
resilience model.

---

## Crash diagnostics

Service crashes write redacted diagnostic captures to `/data/crashes`. Kernel
panics are captured from pstore (`/sys/fs/pstore`) on the first boot after an
unplanned reset. See the [operating.md](operating.md) crash diagnostics section
for usage.

---

## A/B firmware updates

The update subsystem maintains two rootfs slots (A and B). A failed update is
detected by boot count and automatically rolled back to the last known-good
slot. See [providers.md](providers.md) and the update CLI (`nuractl update`)
for details.
