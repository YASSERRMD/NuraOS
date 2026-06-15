# Boot Timing and Log

This document records kernel boot times and log snippets for each phase.
Numbers are measured on the QEMU x86-64 target with default settings
(512 MB RAM, 2 vCPUs, TCG acceleration).

## Phase 04: kernel-only boot to panic

At this phase there is no initramfs. The kernel boots, decompresses,
initialises devices, then panics because no root filesystem is mounted and
no `/init` exists.

Expected serial output (truncated):
```
[    0.000000] Linux version 6.6.x ...
[    0.000000] Command line: console=ttyS0,115200 panic=5 loglevel=7
...
[    0.200000] Serial: 8250/16550 driver, 4 ports, IRQ sharing enabled
[    0.201000] printk: console [ttyS0] enabled
...
[    X.XXXXXX] Kernel panic - not syncing: VFS: Unable to mount root fs on unknown-block(0,0)
```

This confirms:
- Kernel decompresses and starts.
- Early printk and serial console work.
- virtio device detection occurs.
- The kernel reaches the mount-root stage before panicking.

| Metric                         | Value       |
|--------------------------------|-------------|
| Time to serial console active  | ~0.2 s      |
| Time to panic                  | ~2-4 s      |
| bzImage size                   | (see kernel.md) |

## Phase 07: boot to interactive BusyBox shell

At this phase the initramfs is assembled and passed to QEMU. The kernel mounts
it, runs /init, which sets up proc/sysfs/devtmpfs, brings up networking, mounts
/data (or falls back to tmpfs), and drops to a BusyBox shell.

Expected serial output:
```
[    0.200000] printk: console [ttyS0] enabled
...
[init] NuraOS init starting
[init] mounting proc ...
[init] mounting sysfs ...
[init] mounting devtmpfs ...
[init] mounting tmpfs on /tmp ...
[init] bringing up loopback ...
[init] bringing up eth0 (DHCP) ...
[init] eth0: DHCP OK
[init] no /data block device found; /data will be tmpfs (no persistence)
[init] supervisor not found at /sbin/supervisor; dropping to BusyBox shell
/ #
```

| Metric                         | Value       |
|--------------------------------|-------------|
| Time to BusyBox shell prompt   | ~3-5 s      |

## run-qemu.sh invocation for Phase 07

```sh
# Build first:
./scripts/fetch-kernel.sh
./scripts/kernel-config.sh
./scripts/build-kernel.sh
./scripts/fetch-musl.sh
./scripts/fetch-busybox.sh
./scripts/build-initramfs.sh

# Then boot (no /data yet):
./scripts/run-qemu.sh --no-data --timeout 60
```

## Boot log location

The serial log is captured by `run-qemu.sh` to `image/out/boot.log`.
The file is gitignored (build output). Check it locally after a boot run.

## run-qemu.sh invocation for Phase 04

```sh
# Boots kernel only; expects panic (no initramfs)
./scripts/run-qemu.sh --no-initramfs --no-data --timeout 15
```
