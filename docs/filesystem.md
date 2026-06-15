# NuraOS Filesystem Layout

NuraOS uses three distinct storage layers with explicit persistence semantics.

## Layer model

```
+------------------+   read-only by convention
|    initramfs     |   /bin  /sbin  /lib  /usr  /etc (static)
| (in-memory cpio) |
+------------------+
        |
+------------------+   ephemeral tmpfs mounts (reset on every reboot)
|   Runtime layer  |   /run   /tmp   /var
+------------------+
        |
+------------------+   persistent ext4 on virtio-blk (/dev/vda)
|  /data partition |   models, journal, sessions, config, secrets
+------------------+
```

The initramfs is extracted by the kernel into a ramfs before `/init` runs.
OS binaries in `/bin`, `/sbin`, and `/lib` are not written to at runtime --
no NuraOS service opens these paths for writing. This ensures crash safety
and predictable recovery: the OS image is always clean at the next boot.

## Path persistence map

| Path | Mount | Survives reboot? | Contents |
|------|-------|-----------------|----------|
| `/bin` | initramfs | no (OS image) | BusyBox applets and shell utilities |
| `/sbin` | initramfs | no (OS image) | BusyBox system tools, supervisor, service manager, gateway, agent |
| `/lib` | initramfs | no (OS image) | musl libc, kernel modules |
| `/etc` | initramfs | no | Static OS config; resolv.conf persisted via /data bind |
| `/proc` | procfs | virtual | Kernel process information |
| `/sys` | sysfs | virtual | Kernel device/driver information |
| `/dev` | devtmpfs | virtual | Device nodes populated by kernel |
| `/tmp` | tmpfs | **NO** | Scratch space; wiped on reboot |
| `/run` | tmpfs | **NO** | Unix domain sockets, PID files |
| `/var` | tmpfs | **NO** | FHS runtime state; `/var/run` is a symlink to `/run` |
| `/data` | ext4 (virtio-blk) | **YES** | All persistent application data |
| `/data/models` | ext4 | yes | GGUF model files (operator-managed) |
| `/data/journal` | ext4 | yes | NDJSON log records, day-partitioned |
| `/data/sessions` | ext4 | yes | Per-session provenance JSONL (hash-chained) |
| `/data/logs` | ext4 | yes | Legacy log files |
| `/data/etc` | ext4 | yes | `agent.toml`, `secrets.toml`, `hostname`, `timezone`, `resolv.conf` |
| `/data/machine-id` | ext4 | yes | UUID v4 machine identity (non-secret) |

## What resets on reboot

Every restart gives a clean slate for:
- All Unix domain sockets under `/run` (re-created by services on start)
- Temporary files under `/tmp`
- `/var/run`, `/var/lock`, `/var/tmp` (via `/var` tmpfs)
- `/etc/resolv.conf` in memory (restored from `/data/etc/resolv.conf` if present)

## Bind-mounted paths

`/init` binds specific paths from `/data` into the initramfs tree so services
can find them at their canonical locations:

| Source | Destination | Reason |
|--------|-------------|--------|
| `/data/etc/resolv.conf` | `/etc/resolv.conf` | DNS config persists across reboots |

On first boot (no `/data/etc/resolv.conf`), the DHCP-obtained or fallback
`resolv.conf` is saved to `/data/etc/resolv.conf` for future boots.

## Fallback without /data

When no block device is found, `/data` is mounted as a tmpfs. In this mode:
- All data is ephemeral: sessions, logs, and models are lost on shutdown.
- The agent starts normally; a warning is printed to the serial console.
- `nuractl status` still works; disk metrics show 0 bytes (no real partition).

## Read-only rootfs status

The initramfs is a ramfs and cannot be remounted read-only at the kernel level.
Read-only semantics are achieved by convention:

- NuraOS services write only to `/run`, `/tmp`, and `/data`.
- No service opens `/bin`, `/sbin`, `/lib`, `/etc`, or `/usr` for writing.
- The build pipeline (`scripts/build-initramfs.sh`) produces a deterministic
  cpio that is verified against a known checksum for each release.

A future phase will introduce overlayfs to enforce kernel-level read-only for
the base OS layer with a squashfs lower dir.

## fstab

`/etc/fstab` in the initramfs documents the static mounts; `/init` performs
them explicitly rather than relying on mount -a, so the table is informational:

```
proc     /proc  proc     defaults               0 0
sysfs    /sys   sysfs    defaults               0 0
devtmpfs /dev   devtmpfs defaults               0 0
tmpfs    /tmp   tmpfs    defaults               0 0
tmpfs    /run   tmpfs    mode=755,nosuid,nodev  0 0
tmpfs    /var   tmpfs    mode=755,nosuid,nodev  0 0
```

`/data` is not listed in fstab because the device path is probed at runtime.
