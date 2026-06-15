# NuraOS System Identity

NuraOS maintains a stable machine identity that persists across reboots and is
exposed via the `/status` API endpoint.

## Machine ID

The machine-id is a 128-bit random value (UUID v4) stored as a 32-character
lowercase hex string (no dashes) in `/data/machine-id`.

```
a3f8e27c91b04d5f8c62a1e3d70b9284
```

### Properties

- **Stable**: generated once on first boot; reused on subsequent boots.
- **Unique per install**: generated from `crypto/rand`; no two installs share an ID.
- **Non-sensitive**: the ID is random and does not encode any secret, key,
  hardware fingerprint, or user-identifiable information. It may be shared
  freely and logged.
- **Format**: 32 lowercase hex characters. Compatible with the systemd
  machine-id format.

### Location

| Path              | Description |
|-------------------|-------------|
| `/data/machine-id` | Persisted machine-id (source of truth) |

The file is written atomically (write-to-temp + rename) so a power loss during
first-boot generation cannot corrupt it.

## Hostname

The hostname is read from `/data/etc/hostname` at boot. When that file is absent
a default is derived as:

```
nura-<first 8 chars of machine-id>
```

Example: machine-id `a3f8e27c91b04d5f8c62a1e3d70b9284` -> hostname `nura-a3f8e27c`.

### Setting a custom hostname

```sh
echo "mydevice" > /data/etc/hostname
reboot
```

`nura-manager` calls `sethostname(2)` at boot so the kernel hostname matches.

## /status API

The gateway's `GET /status` endpoint includes system identity fields:

```json
{
  "overall": "ok",
  "version": "dev",
  "uptime_seconds": 42,
  "machine_id": "a3f8e27c91b04d5f8c62a1e3d70b9284",
  "hostname": "nura-a3f8e27c",
  "components": [...]
}
```

The `machine_id` and `hostname` fields are omitempty: they are absent when
identity is not initialised (e.g. older gateway binaries).

## System info facility

The `identity.Gather` function assembles a `SysInfo` struct at runtime:

```go
info := identity.Gather(machineID, hostname, startTime)
// SysInfo{
//   MachineID: "a3f8e27c91b04d5f8c62a1e3d70b9284",
//   Hostname:  "nura-a3f8e27c",
//   OSVersion: "1.0.0",
//   Model:     "Standard PC (Q35 + ICH9, 2009)",  // from DMI or device-tree
//   UptimeSec: 42.7,
// }
fmt.Println(info.FormatStatus())
// hostname=nura-a3f8e27c machine_id=a3f8e27c... model=... uptime=42s version=1.0.0
```

`Model` is read from `/sys/class/dmi/id/product_name` (x86) or
`/proc/device-tree/model` (ARM). Falls back to `"unknown"`.

`UptimeSec` is read from `/proc/uptime` (Linux); falls back to
`time.Since(startTime)`.

## Go API

```go
import "github.com/yasserrmd/nuraos/services/internal/identity"

// First-boot: generate and persist. Subsequent boots: read existing.
machineID, err := identity.LoadOrCreate("/data")

// Read configured hostname (or derive from machine-id).
hostname, err := identity.LoadHostname("/data", machineID)

// Apply hostname to the kernel (Linux only; no-op on other platforms).
err = identity.SetHostname(hostname)

// Collect system info snapshot.
info := identity.Gather(machineID, hostname, time.Now())
```
