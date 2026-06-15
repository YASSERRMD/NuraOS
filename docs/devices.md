# NuraOS Device Management

NuraOS uses a two-layer approach to device management:

1. **devtmpfs** -- the kernel populates `/dev` automatically from uevent metadata
   for every discovered device. No userspace help is needed for initial node
   creation.
2. **BusyBox mdev** -- registered as the kernel hotplug helper; applies
   permissions from `/etc/mdev.conf` and triggers shell scripts for block
   device attach/detach events.

This approach requires no udev, no systemd-udevd, and no DBus. It is sufficient
for an appliance with a fixed, well-known device topology.

## Device topology

| Device path | Kernel name | Description |
|-------------|-------------|-------------|
| `/dev/console` | console | Serial console (QEMU `-serial stdio`) |
| `/dev/ttyS0` | ttyS0 | First 8250 UART (serial console) |
| `/dev/vda` | virtio-blk | Boot disk (initramfs image is loaded from here by QEMU, not mounted inside the VM) |
| `/dev/vdb` | virtio-blk | Persistent data disk (default `/data` device) |
| `/dev/urandom` | urandom | Non-blocking entropy source |
| `/dev/null`, `/dev/zero` | -- | Null and zero devices |
| `eth0` | virtio-net | Primary network interface (not a `/dev` node) |

## Deterministic data disk naming

The data disk is accessible via both its kernel name (`/dev/vdb`) and a stable
alias (`/dev/nura-data`) created by `/init` at boot. The alias is a symlink:

```sh
/dev/nura-data -> /dev/vdb
```

This alias is recreated by the hotplug script when the disk is attached late.

### Specifying the data device

By default, `/init` looks for a data disk in this order:

1. `nura.data.dev=` on the kernel command line (explicit, highest priority)
2. `/dev/vdb` -- second virtio block device (standard QEMU layout)
3. `/dev/sda` -- first SCSI disk (fallback for non-virtio kernels)
4. `/dev/vda` -- first virtio disk (single-disk fallback)

To pin the data device explicitly, add to the QEMU `-append` line:

```sh
-append "... nura.data.dev=vdb"
```

The value can be a bare name (`vdb`) or an absolute path (`/dev/vdb`).

The resolved device path is written to `/run/nura-data-dev` so the mdev
hotplug script can mount `/data` if the disk appears after boot.

## mdev configuration

Rules live in `/etc/mdev.conf`. The format is:

```
<regex>  <uid>:<gid>  <permissions>  [@<script_on_add>]
```

Key rules:

| Pattern | Owner | Perms | Note |
|---------|-------|-------|------|
| `vda` | root:root | 0640 | Boot disk (read-only in practice) |
| `vdb` | root:root | 0640 | Data disk; triggers hotplug-block |
| `vd[c-z]` | root:root | 0640 | Additional virtio disks; hotplug |
| `sd[a-z]` | root:root | 0640 | SCSI disks (if enabled in kernel) |
| `urandom` | root:root | 0444 | Entropy; all processes read-only |
| `null`, `zero`, `full` | root:root | 0666 | Standard null devices |
| `ttyS[0-9]*` | root:tty | 0660 | Serial terminals (gid 5) |

## Hotplug

### Block devices

When a block device is added (or removed), `/etc/mdev/hotplug-block` is called
with the environment variables:

| Variable | Value |
|----------|-------|
| `MDEV` | Device name (e.g. `vdb`) |
| `ACTION` | `add` or `remove` |

The script checks `/run/nura-data-dev` to see whether the new device is the
configured data disk. If so:

1. Waits up to 2 seconds for the device to stabilise.
2. Runs `e2fsck -p` to repair any minor filesystem errors.
3. Mounts the device at `/data` with ext4 options `data=ordered,barrier=1,noatime`.
4. Creates `/dev/nura-data` symlink.
5. Ensures the expected subdirectory structure (`models/`, `logs/`, `sessions/`,
   `etc/`, `journal/`) with correct ownership.

On removal, `/data` is unmounted lazily and the symlink is removed.

### Network interfaces

Network interface hotplug is handled by `/init` at boot via a loop over known
interface names (`eth0`, `eth1`). A dedicated hotplug hook for late NIC
attachment is not yet implemented; re-run the network bringup section of init
manually in a recovery shell if a NIC is attached after boot:

```sh
ip link set eth0 up
udhcpc -i eth0 -q -n -t 5 -s /etc/udhcpc/default.script
```

### QEMU hotplug example

To attach the data disk after boot (useful for testing late-attach):

```sh
# On the QEMU host, in the QEMU monitor (Ctrl-A c):
device_add virtio-blk-pci,drive=data,id=data-disk
drive_add 0 file=image/out/data.img,format=raw,id=data
```

The guest `/etc/mdev/hotplug-block` script will detect the `vdb` add event
and mount it at `/data` automatically.

## Kernel configuration

The following kernel options support device management:

| Config | Value | Purpose |
|--------|-------|---------|
| `CONFIG_DEVTMPFS` | y | Kernel auto-populates /dev from uevents |
| `CONFIG_DEVTMPFS_MOUNT` | y | Kernel mounts devtmpfs at boot |
| `CONFIG_VIRTIO_BLK` | y | virtio block driver (vda, vdb) |
| `CONFIG_VIRTIO_NET` | y | virtio network driver (eth0) |
| `CONFIG_VIRTIO_CONSOLE` | y | virtio console (alternative to 8250) |
| `CONFIG_SERIAL_8250` | y | 8250 UART for serial console |
| `CONFIG_TTY` | y | Terminal support |
| `CONFIG_SYSFS` | y | /sys filesystem (required for mdev -s scan) |
| `CONFIG_SCSI` | n | SCSI disabled (virtio-blk covers NuraOS use case) |

## Adding a new device class

1. Add a rule to `/etc/mdev.conf` with the correct regex, uid:gid, and permissions.
2. If the device requires a hotplug action, add a script in `/etc/mdev/` and
   reference it with `@/etc/mdev/<script>` in the rule.
3. Rebuild the initramfs: `./scripts/build-initramfs.sh`.
4. Test by running QEMU and verifying the device node exists with the correct
   permissions after boot.
