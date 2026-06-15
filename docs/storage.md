# NuraOS Storage Model

NuraOS uses two storage layers: a read-only in-RAM initramfs and a writable
persistent ext4 partition.

## Layers

| Mount    | Backing         | Mode       | Contents                                          |
|----------|-----------------|------------|---------------------------------------------------|
| `/`      | initramfs       | read-only  | BusyBox, /init, supervisor, agent, llama-server   |
| `/data`  | virtio-blk ext4 | read-write | models, logs, sessions, config, secrets           |
| `/proc`  | procfs          | virtual    | kernel process information                        |
| `/sys`   | sysfs           | virtual    | kernel device and driver information              |
| `/dev`   | devtmpfs        | virtual    | device nodes populated by the kernel              |
| `/tmp`   | tmpfs           | ephemeral  | process scratch space; lost on reboot             |

## /data layout

```
/data/
    models/     GGUF model files (large, gitignored, fetched separately)
    logs/       boot.log, agent.log (rotated by the agent)
    sessions/   per-session provenance JSONL files (hash-chained)
    etc/        agent.toml     -- main config file
                secrets.toml   -- API keys and gateway token (never committed)
                system_prompt.md -- agent persona
```

## Fallback behaviour

If no `/data` block device is present at boot (for example, in a minimal test
run), `/init` falls back to mounting `/data` as a tmpfs. In this mode:
- All data is lost on reboot.
- The agent still starts, but sessions and logs are not persisted.
- A warning is printed to the serial console.

This ensures `--no-data` boots always succeed cleanly.

## Creating the /data image

```sh
./scripts/make-data-image.sh          # default: 2 GB ext4 image
./scripts/make-data-image.sh --size 512  # smaller image for CI
```

Output: `image/out/data.img` (gitignored).

## Attaching the disk in QEMU

`run-qemu.sh` attaches `image/out/data.img` automatically as a virtio-blk
device (`/dev/vda` in the guest) when the file exists:

```sh
./scripts/run-qemu.sh                 # auto-attaches data.img if present
./scripts/run-qemu.sh --no-data       # boot without /data (tmpfs fallback)
```

## Security considerations

- `/data/etc/secrets.toml` must not be world-readable. The agent refuses to
  start if permissions are too loose (enforced in Phase 33).
- The initramfs is read-only and contains no secrets. All sensitive material
  lives exclusively on `/data`.
- `/data` is not encrypted in the current release. Full-disk encryption is a
  future operator option documented in `/docs/security.md`.

---

## Durability model

### ext4 mount options

`/data` is mounted with options that balance durability and wear:

```
data=ordered,barrier=1,noatime
```

| Option         | Effect |
|----------------|--------|
| `data=ordered` | Data blocks written before journal commits metadata; prevents stale data after crash |
| `barrier=1`    | Write barriers flush the device write cache at journal commit; required for drives with volatile caches |
| `noatime`      | Suppresses access-time writes; reduces flash wear |

### fsck-on-boot policy

Before mounting `/data` read-write, `/init` runs:

```sh
timeout 30 e2fsck -p /dev/vda
```

| Exit code | Meaning | Action |
|-----------|---------|--------|
| 0 | Clean | Mount read-write |
| 1 | Errors fixed | Mount read-write |
| > 1 | Uncorrectable | Mount read-only for recovery |

Boot with `nura.recovery=1` on the kernel cmdline to get a shell and run
`e2fsck -y /dev/vda` manually.

### Atomic write pattern

All config and state files are written atomically to prevent partial files on
crash. The `atomicfile.Write` function in `services/internal/atomicfile`
implements:

1. Write data to a unique temp file in the **same directory** as the target.
2. `fsync()` the temp file (flush to block device).
3. `rename(temp, target)` -- atomic on POSIX.
4. `fsync()` the parent directory (durable directory entry update).

Writers that use this pattern:

| State file | Package |
|------------|---------|
| `/data/machine-id` | `identity` |
| Any config written by services | `atomicfile.Write` directly |

Journal files use `O_APPEND` writes and are fsynced on `Writer.Close()`.
Call `Writer.Sync()` after a burst of critical records for mid-stream durability.

### Power-loss simulation test

```sh
sudo scripts/test-power-loss.sh
```

Creates a 32 MiB ext4 image, writes 20 files using atomic rename, force-unmounts,
and verifies the filesystem with `e2fsck -fn`. Requires root (loop mount).

### Durability guarantees

| Scenario | Guarantee |
|----------|-----------|
| Power loss during config write | Old file intact (rename atomicity) |
| Power loss during journal append | Partial last record only; all prior records intact |
| Power loss during ext4 commit | e2fsck restores metadata consistency on next boot |
| Disk full | Journal rotation drops oldest day files; `atomicfile.Write` fails explicitly |
