# Changelog

All notable changes to NuraOS are documented in this file.

---

## v2.0 (2026-06-15)

NuraOS 2.0 is the first production-ready release. It ships with a fully
integrated OS stack: init system, Go services, llama.cpp inference, and a
complete operational toolchain.

### Highlights

- **Hardened boot chain**: GRUB A/B slot menu, extlinux integration, measured
  boot with signed hash verification, and pstore/ramoops kernel-panic capture.
- **Transactional updates**: block-level binary delta updates, atomic A/B
  firmware apply, rollback to any known-good version, and a full audit log.
- **Inference governance**: cgroup v2 limits on the agent, idle model unload,
  warm model pool, circuit breaker with provider failover.
- **Security posture**: Landlock + seccomp sandbox, capability-aware service
  launcher, automated 10-check security audit (exit 2 on critical failure).
- **Data residency and compliance**: per-provider residency policy, sensitive
  turn blocking, append-only audit log, retention-based deletion.
- **Crash diagnostics**: CrashCap bundles (redacted, rotated), pstore archive,
  diagnostic tar with `nuractl diag`, hardware watchdog with escalation ladder.
- **Config management**: JSON schema with atomic apply, drift detection,
  append-only history.jsonl, and version rollback.
- **Backup and restore**: gzip+tar with AES-256-GCM encryption and SHA-256
  tamper detection using only the Go standard library.
- **Locale and text**: C.UTF-8 default, /data/etc/locale override, U+FFFD
  sanitisation, IsRTL heuristic for Arabic/Hebrew/Thaana/N'Ko.
- **Observability**: Prometheus metrics (all 11 gateway endpoints), Grafana
  dashboard JSON, disk monitor, sysmetrics, inference-governor reporting.
- **Integration matrix**: 10-scenario test runner covering all subsystems,
  auto-gating on resource availability, budget assertions.
- **Performance gates**: reproducible baseline for boot latency, footprint,
  image size, and throughput; CI env-var-driven regression tests.

### New packages (extended phases 56-105)

| Package | Description |
|---------|-------------|
| `internal/selftest` | Built-in health checks (7 checks, boot subset) |
| `internal/crashcap` | Crash bundle capture with secret redaction |
| `internal/paniccap` | pstore/ramoops kernel-panic archiver |
| `internal/diagbundle` | Diagnostic tar assembly |
| `internal/watchdog` | Hardware watchdog and software escalation |
| `internal/configmgr` | Config snapshot, drift detection, rollback |
| `internal/backup` | Encrypted backup and restore |
| `internal/locale` | UTF-8 enforcement and locale config |
| `internal/secaudit` | 10-check security posture auditor |
| `internal/compliance` | Data-residency policy, audit log, retention |
| `internal/integtest` | Integration test matrix runner |
| `internal/perf` | Performance budget gates and baseline |

### New nuractl subcommands

| Command | Description |
|---------|-------------|
| `selftest [--boot] [--category ...]` | Run built-in health self-tests |
| `secaudit [--critical]` | Run security posture audit |
| `data delete-expired` | Delete data older than retention window |
| `data compliance-report` | Per-turn provider handling report |
| `diag` | Build redacted diagnostic archive |
| `backup` | Create encrypted /data backup |
| `restore` | Restore /data from backup archive |
| `integtest` | Run full system integration matrix |
| `perf` | Evaluate performance regression gates |

### Documentation added (phases 94-105)

- `docs/testing.md` -- integration matrix, QEMU CI, budget assertions
- `docs/performance.md` -- 2.0 baseline, regression gates, tuning notes
- `docs/handbook.md` -- operator handbook and incident runbooks
- `docs/index.md` -- unified documentation index (40+ docs)
- `docs/compliance.md` -- data residency policy and audit log
- `docs/resilience.md` -- watchdog, escalation ladder, crash loop handling
- `docs/locale.md` -- UTF-8 policy, RTL text, serial console guidance
- `docs/security.md` (appended) -- security audit table, CI integration

---

## v1.0 (2025-12-01)

Initial internal milestone. Covered the core OS stack:

- musl/BusyBox rootfs, init system, nura-manager, nura-agent
- Go gateway (11 endpoints), llama.cpp inference, provider abstraction
- A/B boot slots, signed packages, firewall, and observability MVP
