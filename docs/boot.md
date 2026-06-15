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

## Phase 07: boot to interactive shell

Updated when Phase 07 (initramfs) is complete.

| Metric                         | Value       |
|--------------------------------|-------------|
| Time to BusyBox shell prompt   | TBD         |

## Boot log location

The serial log is captured by `run-qemu.sh` to `image/out/boot.log`.
The file is gitignored (build output). Check it locally after a boot run.

## run-qemu.sh invocation for Phase 04

```sh
# Boots kernel only; expects panic (no initramfs)
./scripts/run-qemu.sh --no-initramfs --no-data --timeout 15
```
