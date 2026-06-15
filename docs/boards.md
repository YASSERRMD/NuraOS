# NuraOS Hardware Targets

NuraOS ships board configuration files in `boards/` that describe CPU flags,
RAM requirements, and machine parameters for each supported target. The build
system and boot scripts read the appropriate file via the `BOARD` environment
variable.

## Supported boards

| Board ID | Name | Arch | Min RAM | Notes |
|---|---|---|---|---|
| `qemu-x86_64` | QEMU x86-64 | x86_64 | 512 MB | Default dev / CI target |
| `rpi4` | Raspberry Pi 4 Model B | aarch64 | 2 GB | 4 GB variant recommended |
| `rpi5` | Raspberry Pi 5 | aarch64 | 4 GB | 8 GB supports 3B-param models |
| `generic-arm64` | Generic ARM64 | aarch64 | 2 GB | Graviton, T2A, Ampere, etc. |

## Board config schema

Each `boards/<id>.json` file contains:

```json
{
  "id":                "qemu-x86_64",
  "name":              "QEMU x86-64 (development / CI)",
  "arch":              "x86_64",
  "cpu":               "qemu64",
  "cpu_flags":         ["-DGGML_NATIVE=OFF", "-DGGML_AVX=ON"],
  "min_ram_mb":        512,
  "recommended_ram_mb": 2048,
  "vcpus":             2,
  "storage":           "virtio-blk",
  "machine":           "q35",
  "accel":             "tcg",
  "notes":             "..."
}
```

`cpu_flags` are passed to the llama.cpp cmake invocation so the GGML backend
uses the right SIMD instructions for the target CPU.

## Writing board info at boot

During the first boot (or after reimaging), write the board info file:

```sh
# Auto-detect from /proc/cpuinfo and /proc/device-tree/model
bash scripts/board-info.sh --detect --write

# Or specify explicitly
bash scripts/board-info.sh --board-id rpi5 --write
```

The file is written to `/data/etc/board.json` by default.

Override paths:

```sh
bash scripts/board-info.sh --board-id rpi4 \
    --boards-dir ./boards \
    --board-info /tmp/board.json \
    --write
```

## Listing boards

```sh
bash scripts/board-info.sh --list
```

## Reading board info

```sh
bash scripts/board-info.sh           # prints /data/etc/board.json
bash scripts/board-info.sh --board-id qemu-x86_64  # prints from boards/ directly
```

## Gateway endpoint

```
GET /board
```

Returns the active board info or `null` before first configuration:

```json
{
  "board": {
    "id": "rpi5",
    "name": "Raspberry Pi 5",
    "arch": "aarch64",
    "cpu": "cortex-a76",
    "cpu_flags": ["-DGGML_NATIVE=OFF", "-DGGML_NEON=ON"],
    "min_ram_mb": 4096,
    "recommended_ram_mb": 8192,
    "vcpus": 4,
    "storage": "nvme-or-sd",
    "machine": "raspi5",
    "accel": "native",
    "notes": "..."
  }
}
```

Override the path read by the gateway:

```sh
BOARD_INFO_FILE=/custom/board.json ./gateway
```

## Adding a new board

1. Create `boards/<new-id>.json` with the required fields.
2. Set `cpu_flags` to the cmake flags appropriate for the CPU.
3. Test with `bash scripts/board-info.sh --board-id <new-id>`.
4. Run `bash scripts/board-info.sh --board-id <new-id> --write` on first boot.
