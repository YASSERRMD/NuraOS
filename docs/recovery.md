# NuraOS Recovery Guide

This document describes the NuraOS recovery and rescue environment: how to
enter it, what tools are available, and how to perform common recovery tasks.

---

## Overview

NuraOS provides two recovery modes, both selectable from the extlinux boot menu
or via the kernel cmdline:

| Mode | Cmdline flag | When to use |
|------|-------------|-------------|
| Full recovery | `nura.recovery=1` | Normal recovery: fsck, rollback, factory reset |
| Early recovery | `nura.recovery=early` | When /data itself is unresponsive or inaccessible |

**Neither mode starts the agent, supervisor, or networking.** This limits
attack surface and prevents unintended outbound connections while the system
is in a maintenance state.

---

## Entering recovery mode

### From the extlinux boot menu

On QEMU, interrupt the 5-second autoboot countdown by pressing any key, then
select "NuraOS -- Recovery Mode" from the menu.

To make recovery the default for the next boot only:

```sh
./scripts/boot-config.sh --recovery
./scripts/build-boot.sh
./scripts/run-qemu.sh --disk image/out/disk.img
```

### Via kernel cmdline (QEMU direct-kernel mode)

```sh
# Full recovery (recommended):
./scripts/run-qemu.sh --kernel image/out/bzImage \
    --initramfs image/out/initramfs.cpio.gz \
    --extra-append "nura.recovery=1"
```

### From a running system

`nuractl` can arrange the next boot to enter recovery:

```sh
./scripts/boot-config.sh --recovery
# Reboot the system; extlinux will default to the recovery entry.
```

---

## Recovery is always reachable

Because recovery runs entirely from the initramfs (not from the A/B rootfs
slots), it is reachable even when:

- The active rootfs slot (`/boot/rootfs-a.ext4` or `/boot/rootfs-b.ext4`) is
  corrupted or unbootable.
- The supervisor or agent binary is missing or broken.
- The gateway fails to start.
- A failed update left the system in an inconsistent state.

The only requirement is that the initramfs and kernel are intact (which the
extlinux boot menu and integrity check protect).

---

## Full recovery console (`nura.recovery=1`)

After mounting essential virtual filesystems and attempting to mount `/data`,
`init` launches `/sbin/nura-recovery`. The console presents an interactive menu:

```
=====================================================
  NuraOS Recovery Console v1.0
=====================================================

  Slot:    a
  Kernel:  6.6.x
  Uptime:  4s

  Network is DISABLED. Agent is NOT running.

Recovery options:
  1) Open shell             (BusyBox sh)
  2) Check /data filesystem (e2fsck)
  3) View update history
  4) Rollback to other slot
  5) View journal log
  6) Factory reset /data
  q) Reboot
```

### Option 1: Open shell

Opens a BusyBox sh with access to all initramfs tools. Type `exit` to return
to the menu.

```sh
# Useful commands in the recovery shell:
ls /data/etc/           # inspect config
cat /data/journal/nura.log  # view journal
sha256sum -c /data/etc/boot-hashes  # check integrity manually
mount                   # list mounted filesystems
```

### Option 2: Check /data filesystem (e2fsck)

Unmounts `/data`, runs `e2fsck -f` on the underlying block device, and
remounts. Use this if the journal shows ext4 errors or the system reported
fsck errors at last boot.

### Option 3: View update history

Prints `/data/update/history.json` -- the version history store populated by
`nuractl update apply`. Shows all past updates with slots, SHA-256 hashes, and
known-good markers.

### Option 4: Rollback to other slot

Flips `/data/etc/active-slot` to the other slot (A->B or B->A). On the next
reboot, the system will boot from the other slot. Use this when the current
slot is broken and the other slot is known-good.

### Option 5: View journal log

Shows the last 50 lines of `/data/journal/nura.log`. Useful for diagnosing
why the last normal boot failed.

### Option 6: Factory reset /data

**Destructive.** Wipes `/data` contents (sessions, logs, models, packages,
config, journal) and recreates the standard directory structure. Requires
double confirmation by typing `RESET` twice.

Machine-ID policy: if `/data/etc/factory-reset.conf` contains
`preserve_machine_id=yes`, the machine-id is backed up and restored after the
wipe. Otherwise it is regenerated on the next boot.

After a factory reset, the system writes `/data/etc/factory-reset-at` with the
timestamp so that first-boot provisioning logic can detect and act on it.

---

## Early recovery console (`nura.recovery=early`)

For extreme cases where `/data` itself is unresponsive or the block device is
inaccessible, `nura.recovery=early` drops to a BusyBox shell immediately after
mounting `/proc`, `/sys`, and `/dev` -- before any attempt to mount `/data` or
bring up networking.

```sh
# From early recovery shell:
ls /dev/disk/           # list block devices
blkid /dev/vdb          # check /data partition signature
dumpe2fs /dev/vdb       # inspect ext4 superblock
mount -t ext4 -o ro /dev/vdb /mnt  # attempt manual mount
```

Exiting the early recovery shell initiates a reboot.

---

## Recovery procedures

### Procedure: recover from a failed update

1. Boot with `nura.recovery=1`.
2. Select option 3 (View update history) to identify the last known-good slot.
3. Select option 4 (Rollback to other slot) if the other slot is known-good.
4. Select `q` to reboot.

Alternatively, from a running system before the problem occurs:

```sh
# Mark current state as known-good (do this when the system is healthy):
nuractl history mark-good <id>

# Roll back from the command line:
nuractl history rollback <id>
```

### Procedure: /data filesystem errors

1. Boot with `nura.recovery=1`.
2. Select option 2 (Check /data filesystem).
3. Follow e2fsck output; if errors are fixed, reboot normally.
4. If e2fsck cannot repair, consider factory reset (option 6).

### Procedure: boot integrity failure

If the system drops to a recovery shell with the message
"boot integrity check FAILED":

1. Examine which file failed:
   ```sh
   sha256sum -c /data/etc/boot-hashes
   ```
2. If the mismatch is from a legitimate update (manifest not regenerated):
   ```sh
   # Regenerate manifest from the recovery shell:
   ./scripts/sign-rootfs.sh --key "$(cat /etc/nura/boot.priv.hex)" \
       --slot a image/out/bzImage image/out/initramfs.cpio.gz
   ```
3. If the mismatch is unexpected, the system may be compromised: perform
   a factory reset and restore from a known-good image.

---

## Factory reset: machine-ID policy

Create `/data/etc/factory-reset.conf` to control machine-ID behaviour on reset:

```sh
# /data/etc/factory-reset.conf
# Set to "yes" to retain the machine-id across factory resets.
preserve_machine_id=yes
```

When `preserve_machine_id=yes` is set, the machine-id from
`/data/etc/machine-id` is backed up before the wipe and restored after, so
the device retains its identity. This is useful for IoT deployments where the
machine-id is used for cloud registration or device tracking.

When `preserve_machine_id` is absent or not `yes`, the machine-id is wiped and
a new one is generated at first boot.
