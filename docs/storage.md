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

---

## Space policy

The goal of the space policy is to ensure `/data` never fills to the point that
the appliance becomes non-functional.

### Thresholds

| Level    | Default | Behaviour |
|----------|---------|-----------|
| Warn     | 80% full | Auto-reclaim triggered; logged at Warn severity |
| Critical | 95% full | New chat sessions refused (HTTP 503); logged at Error severity |

### Disk monitor

`diskmon.Monitor` in `services/internal/diskmon` polls the `/data` filesystem
every 30 seconds using `syscall.Statfs`. State transitions (ok -> warn, warn ->
critical, and recoveries) fire once on change and are broadcast to registered
callbacks. The gateway reads the most recent snapshot for `/status` and
`/metrics` without blocking.

### Per-subtree soft quotas

| Subtree | Default cap | Reclaim strategy |
|---------|-------------|------------------|
| `/data/sessions` | 512 MiB | Delete oldest session JSONL files first |
| `/data/logs` | 128 MiB | Delete oldest log files first |
| `/data/journal` | 100 MiB | Journal writer auto-rotates oldest day files |
| `/data/models` | (unlimited) | Operator managed; models are not auto-deleted |

Quotas are enforced by `diskmon.Reclaim` which walks each subtree, sorts files
by modification time (oldest first), and removes files until the subtree is
within its cap. The journal is not in the reclaim path because `journal.Writer`
already enforces its own 100 MiB cap on every write.

### Automatic reclaim

When the disk transitions from ok to warn, `nura-manager` calls `Reclaim`
automatically with the default per-subtree caps above. The freed bytes and
resulting usage are logged.

### Manual reclaim

```sh
nuractl reclaim              # trim sessions (512 MiB cap) and logs (128 MiB cap)
nuractl reclaim --json       # same, JSON output: {"freed_bytes":N,"data_dir":"/data"}
```

`NURA_DATA_DIR` overrides the default `/data` root when set.

### Disk metrics

The gateway exposes disk metrics in Prometheus text format at `GET /metrics`:

```
nura_disk_total_bytes     <total filesystem bytes>
nura_disk_used_bytes      <bytes in use>
nura_disk_available_bytes <bytes available to processes>
nura_disk_used_percent    <float, 0-100>
```

And in the JSON health summary at `GET /status`:

```json
{
  "disk": {
    "path": "/data",
    "total_bytes": 2147483648,
    "used_bytes": 536870912,
    "free_bytes": 1610612736,
    "used_pct": 25.0,
    "status": "ok"
  }
}
```

`status` is `"ok"`, `"warn"`, or `"critical"` and mirrors the monitor state.
