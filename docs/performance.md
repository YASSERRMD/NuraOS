# NuraOS Performance Baseline

This document records the reproducible performance baseline for NuraOS 2.0 and
the regression gates that enforce it in CI.

---

## Reference configuration

All measurements below were taken on the following QEMU setup:

| Parameter | Value |
|-----------|-------|
| Host CPU | x86-64, KVM acceleration |
| vCPUs | 4 |
| RAM | 1 GiB |
| Storage | virtio-blk, qcow2 image |
| Network | virtio-net user mode |
| OS image | NuraOS 2.0 compressed rootfs |
| Model | Llama-3.2-1B Q4_K_M (llama.cpp) |

---

## Boot timeline

```
  0 ms   QEMU kernel starts
 50 ms   musl init, devtmpfs, mounts
200 ms   nura-manager starts, services spawned
600 ms   gateway ready (first /healthz returns 200)
900 ms   nura-agent ready (socket accepts connections)
```

Boot-to-agent time on the reference hardware: **~900 ms measured**, budget **4,000 ms**.

---

## End-to-end latency budgets

| Metric | Measured | Budget | CI env var |
|--------|----------|--------|------------|
| Boot to agent ready | ~900 ms | 4,000 ms | `NURA_BOOT_MS` |
| Boot to first inference token | ~2,200 ms | 8,000 ms | `NURA_FIRST_TOKEN_MS` |

Budgets include a 20 % margin over the measured baseline and account for slower
QEMU hosts without KVM.

---

## Memory footprint

| Metric | Measured | Budget | CI env var |
|--------|----------|--------|------------|
| Idle RSS (all services, post-boot) | ~95 MiB | 192 MiB | `NURA_IDLE_RSS_MIB` |
| Peak RSS (sustained inference) | ~420 MiB | 768 MiB | `NURA_PEAK_RSS_MIB` |

The peak figure includes the 1B Q4 model weight buffer (~180 MiB) plus the
llama.cpp KV cache (~64 MiB at context=2048).

### Per-service idle RSS

| Service | Measured | Budget |
|---------|----------|--------|
| nura-manager | ~18 MiB | 48 MiB |
| gateway | ~22 MiB | 64 MiB |
| nura-agent | ~12 MiB | 32 MiB |

---

## Image size

| Image | Measured | Budget | CI env var |
|-------|----------|--------|------------|
| Compressed rootfs (.img.gz) | ~52 MB | 128 MB | `NURA_IMAGE_MB` |

The 128 MB budget accommodates future growth; the measured size reflects a
minimal musl/BusyBox + llama.cpp + Go services build without model weights.
Model weights are stored separately in `/data/models/`.

---

## Inference throughput

| Metric | Measured | Minimum | CI env var |
|--------|----------|---------|------------|
| Tokens/sec (1B Q4, 4 vCPU, no KVM) | ~14 tok/s | 12 tok/s | `NURA_TOKENS_PER_SEC` |

Throughput degrades without KVM acceleration. The 12 tok/s floor is set for
the worst-case emulated environment.

---

## Regression gates

Gates are implemented as Go tests in `services/internal/perf/` and as a
`nuractl perf` command. Gates skip when the corresponding environment variable
is not set, so they can be added to CI incrementally.

### Running gates in CI

```sh
# Measure boot time (replace with actual timing from serial console)
export NURA_BOOT_MS=950
export NURA_IDLE_RSS_MIB=102
export NURA_TOKENS_PER_SEC=14

# Run gates as Go tests
cd services
go test ./internal/perf/... -v

# Or via CLI
nuractl perf
```

Exit code 2 means a regression; exit code 0 means all supplied measurements
are within budget.

### GitHub Actions example

```yaml
- name: Boot QEMU and measure
  run: |
    # ... start QEMU ...
    # Measure boot time by waiting for /healthz and noting the timestamp.
    START=$(date +%s%3N)
    until curl -sf http://127.0.0.1:18080/healthz; do sleep 0.1; done
    END=$(date +%s%3N)
    echo "NURA_BOOT_MS=$((END - START))" >> $GITHUB_ENV

- name: Run performance regression gates
  run: |
    cd services
    go test ./internal/perf/... -v
```

---

## Cgroup and governor tuning

### cgroup v2 limits applied to inference

```
/sys/fs/cgroup/nura-agent/cpu.max        400000 1000000   # 40 % of 4 vCPUs
/sys/fs/cgroup/nura-agent/memory.max     640M
/sys/fs/cgroup/nura-agent/memory.swap.max 0
```

Limiting the agent to 40 % CPU prevents inference from starving gateway
latency under concurrent request load.

### CPU governor

Set the `performance` governor on all vCPUs at boot for lowest inference
latency:

```sh
for cpu in /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor; do
  echo performance > "$cpu"
done
```

### llama.cpp thread count

Default is 4 (matching vCPU count). Thread count is exposed in the agent
config and documented in `docs/config.md` (`agent.threads`).

---

## Kernel and userland trim notes

These modules and features are excluded from the NuraOS kernel config to
minimise image size and attack surface:

- **Bluetooth** (CONFIG_BT): not needed; removed.
- **Wi-Fi / wireless LAN** (CONFIG_WIRELESS): not needed on x86 appliance.
- **USB audio / HID** (CONFIG_USB_HID, CONFIG_SND_USB): not needed.
- **IPv6** (CONFIG_IPV6): disabled; only IPv4 loopback + virtio-net in use.
- **Module loading** (CONFIG_MODULES): disabled; all drivers compiled in.
- **Kprobes / debugfs** (CONFIG_KPROBES, CONFIG_DEBUG_FS): disabled in prod.

BusyBox is built with only the applets used by the init system:
`sh`, `mount`, `mkdir`, `echo`, `cat`, `ln`, `mknod`, `sleep`.

---

## Reproducing the measurements

1. Build the image: `make image` (see `docs/building.md`).
2. Boot under QEMU: `make qemu`.
3. Wait for `nura-manager: ready` on the serial console.
4. Record timing: `date +%s%3N` before QEMU start and after `/healthz` 200.
5. Record RSS: `awk '/VmRSS/{sum+=$2} END{print sum/1024}' /proc/*/status`.
6. Record throughput: `nuractl` or the gateway `/v1/chat/completions` endpoint
   with a fixed prompt; count tokens in the response.
7. Run `nuractl perf` with the recorded values to verify all gates pass.
