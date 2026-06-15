# Suite: build-and-boot

Asserts that the NuraOS build pipeline produces correct artifacts and that the
system boots to a fully working agent. Each case maps back to the build-phase
acceptance criteria it exercises.

## Cases

| Case | Phase | Assertion |
| --- | --- | --- |
| `kernel-size` | 01 | `bzImage` exists and is 1 MB -- 100 MB |
| `image-assembly` | 01/03/05/06 | All 4 artifacts present in `image/out/` |
| `boot-ready` | 10 | `/healthz` returns 200 on the pre-booted instance |
| `serial-repl` | 35 | REPL responds to `:help` with command list within 10 s |
| `data-mounted` | 05 | `/data` ext4 partition mounts on a fresh boot with `data.img` attached |
| `offline-boot` | 00/01 | Supervisor starts with no network device attached |

## How to run

```sh
NURA_REPO_ROOT=/path/to/nuraos tests/run-suite build-and-boot
```

Reports are written to `tests/reports/build-and-boot/`.

## Notes

- `data-mounted` and `offline-boot` each boot a separate QEMU instance inside
  the suite. The test run therefore starts three QEMU VMs in total. Ensure the
  host has enough RAM (>=768 MB free) when running locally.
- `data-mounted` skips with `StatusSkip` when `image/out/data.img` is absent
  so the suite can still run against a kernel-only build.
- `offline-boot` uses `NoNetwork: true` which omits the virtio-net device; the
  agent is not HTTP-reachable but the supervisor start is verified via serial.
