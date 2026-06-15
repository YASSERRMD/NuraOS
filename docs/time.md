# NuraOS Time Model

NuraOS provides a layered time subsystem that ensures services receive accurate
and coherent timestamps even on embedded hardware with a battery-backed RTC,
and continues to boot correctly when no network is available.

## Boot sequence

```
1. Load timezone (/data/etc/timezone -> time.Local + TZ env)
2. Read RTC (/dev/rtc0) -> settimeofday (best-effort; needs CAP_SYS_TIME)
3. If NURA_NTP_SERVER is set -> one-shot SNTP query -> settimeofday
4. Background: clock-step logger + 30-minute NTP refresh (if NTP enabled)
```

Steps 2-4 are non-fatal: if the RTC is absent or the NTP server is
unreachable, nura-manager continues with the current system time.

## Monotonic clock

Every component should obtain timestamps via `Clock.Now()` rather than
`time.Now()` directly. The returned `MonoTime` carries both:

| Field  | Type         | Description |
|--------|--------------|-------------|
| `Wall` | `time.Time`  | Wall clock reading (UTC, nanosecond precision) |
| `Seq`  | `uint64`     | Per-process monotonic sequence number (starts at 1) |

The sequence number survives clock steps and can be used for causal ordering
when two records have identical or ambiguous wall clock timestamps.

```go
ts := timeMgr.Now()
fmt.Printf("event at wall=%s seq=%d\n", ts.Wall.Format(time.RFC3339Nano), ts.Seq)
```

## Clock-step detection

The `Clock` continuously compares the monotonic elapsed time (Go's
`time.Time.Sub` uses the runtime monotonic reading) against the wall elapsed
time. When the discrepancy exceeds one second the event is sent on the
`StepEvents()` channel and logged at warning level:

```
nura-manager: clock step detected before=2025-01-15T10:00:00Z after=2025-01-15T10:00:30Z delta=+29.99s seq=142
```

A positive delta means the wall clock jumped forward (e.g. NTP step-up after
network came up). A negative delta means the clock was set back.

## NTP synchronisation

NTP sync is opt-in and never required for offline boot.

```sh
# Enable by setting the env var before starting nura-manager:
NURA_NTP_SERVER=pool.ntp.org nura-manager run
# or a specific server:
NURA_NTP_SERVER=time.cloudflare.com
```

The client implements SNTP (RFC 4330): a single UDP packet exchange on
port 123. It is not a full NTP implementation; it does not do slewing or
drift correction. Initial sync happens at startup; subsequent syncs run
every 30 minutes.

## Timezone configuration

Write a POSIX timezone name to `/data/etc/timezone`:

```sh
echo "America/Los_Angeles" > /data/etc/timezone
```

On the next boot `nura-manager` reads this file, sets `time.Local`, and
exports `TZ` so child processes inherit it. The default is `UTC`.

```sh
# Available timezone names come from the IANA database (tzdata).
# On the rootfs, tzdata is expected at /usr/share/zoneinfo/.
```

## RTC device

The hardware clock is read from `/dev/rtc0` at boot via the `RTC_RD_TIME`
ioctl. If the device is absent (e.g. QEMU without `-rtc` option, or
insufficient permissions), nura-manager logs a warning and continues with
the kernel's existing system time.

## Go API

```go
import "github.com/yasserrmd/nuraos/services/internal/timesync"

mgr := timesync.NewManager(log)
_ = mgr.Start(ctx, timesync.Config{
    NTPServer:  "pool.ntp.org",   // empty = offline
    TZFile:     "/data/etc/timezone",
    RTCDevice:  "/dev/rtc0",
})

// Get a timestamped + sequenced reading.
ts := mgr.Now()                    // timesync.MonoTime

// Access the clock directly.
clock := mgr.Clock()
for step := range clock.StepEvents() {
    log.Warn("clock jumped", "delta", step.Delta)
}
```
