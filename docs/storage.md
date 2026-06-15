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
