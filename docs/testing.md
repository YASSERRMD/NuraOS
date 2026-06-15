# NuraOS Testing Guide

This document covers all layers of the NuraOS test suite: unit tests, the
integration matrix, headless QEMU CI, and boot/footprint budget assertions.

---

## Test layers

| Layer | Location | Run command |
|-------|----------|-------------|
| Unit tests | `services/internal/*/` | `go test ./...` |
| Integration matrix | `services/internal/integtest/` | `go test ./internal/integtest/...` |
| Budget assertions | `services/internal/integtest/budget_test.go` | included above |
| CLI smoke matrix | `scripts/smoke-test.sh` | `./scripts/smoke-test.sh` |
| Headless QEMU | `ci/qemu-matrix.sh` | see QEMU CI section below |

---

## Running unit tests

```sh
cd services
go test ./...
```

All packages must pass with zero failures. Tests that require kernel features
(cgroup v2, seccomp, pstore) skip automatically when running outside the
NuraOS appliance.

---

## Integration matrix

The integration matrix exercises end-to-end scenarios across subsystems. Each
scenario is self-gating: if the required resource (gateway, agent socket, /data
mount) is absent, the scenario skips rather than fails.

### Run locally (no appliance)

```sh
cd services
go test ./internal/integtest/...
```

Scenarios that require a live gateway or agent will skip. The storage
durability and self-test scenarios always run.

### Run against a live appliance

Set environment variables before running:

```sh
export NURA_GATEWAY_URL=http://nura.local:8080
export NURA_AGENT_SOCKET=/run/nura-agent.sock
export NURA_DATA_DIR=/data

cd services
go test ./internal/integtest/... -v
```

Or via the CLI:

```sh
nuractl integtest --gateway http://nura.local:8080
```

Exit code 2 means one or more scenarios failed. Exit code 0 means all
non-skipped scenarios passed.

### Subsystem coverage

| Scenario | Subsystem | Requires |
|----------|-----------|----------|
| `gateway-healthz` | service-lifecycle | Live gateway |
| `gateway-version` | service-lifecycle | Live gateway |
| `gateway-status` | observability | Live gateway |
| `gateway-metrics` | observability | Live gateway |
| `agent-socket-reachable` | service-lifecycle | Running agent |
| `selftest-boot-subset` | integration | Always runs |
| `storage-durability` | storage | Always runs |
| `provider-health-snapshot` | provider-failover | Live gateway |
| `crash-dir-exists` | resilience | /data mounted |
| `model-dir-accessible` | model-lifecycle | /data mounted |

### Adding scenarios

Register custom scenarios with `Runner.Register`:

```go
r := integtest.New(gatewayURL, agentSocket)
r.Register(&integtest.Scenario{
    Name:      "my-subsystem-check",
    Subsystem: "my-subsystem",
    Run: func(ctx context.Context) integtest.ScenarioResult {
        // ...
        return integtest.ScenarioResult{Status: integtest.ScenarioPass}
    },
})
rep := r.Run(ctx)
```

---

## Boot/footprint budget assertions

`budget_test.go` asserts that the integration matrix binary itself stays within
memory and goroutine budgets:

| Budget | Limit | Env to skip |
|--------|-------|-------------|
| Heap in use | 64 MiB | `NURA_SKIP_BUDGET=1` |
| Goroutines | 128 | `NURA_SKIP_BUDGET=1` |

These catch memory leaks in scenarios. Set `NURA_SKIP_BUDGET=1` to skip them
in constrained CI environments.

---

## Headless QEMU CI

To run the full matrix against a headless appliance image in CI:

1. Start QEMU with a serial console redirect and a host-side port forward:

```sh
qemu-system-x86_64 \
  -nographic \
  -drive file=nuraos.img,format=qcow2 \
  -m 512M \
  -netdev user,id=n0,hostfwd=tcp::18080-:8080 \
  -device virtio-net,netdev=n0 \
  -serial stdio
```

2. Wait for the readiness line (the appliance prints `nura-manager: ready` on
   the serial console when all core services are healthy).

3. Run the integration matrix pointing at the forwarded port:

```sh
NURA_GATEWAY_URL=http://127.0.0.1:18080 \
  go test ./internal/integtest/... -v -timeout 120s
```

4. Exit code 0 from `go test` means all scenarios passed or skipped. Exit code
   2 from `nuractl integtest` means failures.

### Readiness gating

To prevent false failures on slow boot, wait for the gateway healthz endpoint
before running the matrix:

```sh
until curl -sf http://127.0.0.1:18080/healthz; do sleep 2; done
go test ./internal/integtest/... -v -timeout 120s
```

### CI example (GitHub Actions)

```yaml
- name: Boot QEMU appliance
  run: |
    qemu-system-x86_64 -nographic -drive file=nuraos.img,format=qcow2 \
      -m 512M -netdev user,id=n0,hostfwd=tcp::18080-:8080 \
      -device virtio-net,netdev=n0 -serial stdio &
    until curl -sf http://127.0.0.1:18080/healthz; do sleep 2; done

- name: Run integration matrix
  run: |
    cd services
    NURA_GATEWAY_URL=http://127.0.0.1:18080 go test ./internal/integtest/... -v
```

---

## CLI smoke matrix

`scripts/smoke-test.sh` provides a shell-level smoke test against a live
gateway. It exercises all 11 REST endpoints and emits pass/fail/skip per line.

```sh
NURA_GATEWAY_URL=http://127.0.0.1:8080 ./scripts/smoke-test.sh
```

See `docs/operating.md` for the full smoke test catalogue.

---

## Skipping kernel-only checks

Many unit checks in `services/internal/selftest/` read Linux-specific paths
(`/proc/sys/kernel/seccomp/actions_avail`, `/sys/fs/cgroup/cgroup.controllers`,
etc.). They skip automatically on macOS/Windows. The `--boot` flag limits the
self-test to a subset that always runs on the appliance.

---

## Test package summary

| Package | Tests | Key fixtures |
|---------|-------|--------------|
| `selftest` | 8 | Linux-only; skips on non-Linux |
| `crashcap` | 4 | tmpdir |
| `paniccap` | 5 | fake pstore in tmpdir |
| `diagbundle` | 4 | tmpdir |
| `watchdog` | 6 | software-only mode |
| `configmgr` | 12 | tmpdir |
| `backup` | 7 | tmpdir |
| `locale` | 12 | in-process |
| `secaudit` | 9 | skips privileged checks |
| `compliance` | 9 | tmpdir |
| `integtest` | 8 + 2 budget | fake httptest gateway |
