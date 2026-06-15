# NuraOS Test Harness

Automated test system for NuraOS. Tests run in GitHub Actions with NuraOS
booted headless in QEMU. Failing suites file or update GitHub issues via
the `gh` CLI.

## Directory layout

```
tests/
  harness/            Shared Go library: QEMU boot, HTTP/serial clients, result types
  reporters/          JUnit XML and JSON report writers
  cmd/run-suite/      Entry point: run-suite <suite-name>
  suites/             One subfolder per subsystem suite (populated by T04+)
  fixtures/           Static test data shared across suites
  reports/            Runtime output (gitignored)
  adr/                Architecture decision records
  go.mod              Module: github.com/yasserrmd/nuraos/tests (stdlib only)
```

## Building the harness

```sh
cd tests
go build ./cmd/run-suite
```

This produces `./run-suite` with no external dependencies.

## Running a suite

```sh
# From repo root (run-suite walks up to find it):
./tests/run-suite smoke

# Or with explicit repo root:
NURA_REPO_ROOT=/path/to/nuraos ./tests/run-suite smoke
```

Reports are written to `tests/reports/<suite>/`:
- `<suite>-junit.xml` -- JUnit XML (GitHub Actions test annotations)
- `<suite>-report.json` -- Full JSON report with per-case results and evidence paths

## Result schema

Every test case produces a `Result` (JSON):

```json
{
  "run_id":            "a3f8c1d2e4b70912",
  "commit_sha":        "5f99537...",
  "suite":             "build-and-boot",
  "case":              "healthz",
  "status":            "fail",
  "duration_ms":       234.5,
  "message":           "/healthz returned 503 (want 200)",
  "failure_signature": "8c3a1b2d4e5f6789",
  "evidence": {
    "bundle_dir":       "tests/reports/build-and-boot/healthz-evidence",
    "serial_log_path":  "tests/reports/.../serial.log",
    "journal_excerpt":  "...(last 100 lines of serial output)...",
    "metrics_snapshot": "tests/reports/.../metrics.txt",
    "config_dump":      "tests/reports/.../config.json"
  }
}
```

Key fields:

| Field | Description |
| --- | --- |
| `run_id` | Random 16-char hex shared by all results in one `run-suite` invocation |
| `commit_sha` | `git rev-parse HEAD` at run start |
| `failure_signature` | `SHA-256(suite:case:Normalise(message))[:8]` -- stable across runs, used for GitHub issue dedup |
| `evidence.bundle_dir` | Directory containing all captured files for this failure |
| `evidence.journal_excerpt` | Last 100 lines of the serial log (inline) |

The `failure_signature` normaliser strips timestamps, PIDs, hex addresses, and
high-numbered port numbers from the message before hashing, so the same logical
failure produces the same signature regardless of run-specific values.

All evidence text is redacted before writing (API keys, Bearer tokens, and
config secret values are replaced with `[REDACTED]`).

## Design principles

- **No fixed sleeps.** `WaitReady` polls `/healthz` at 500 ms intervals.
  `WaitForPattern` polls the serial buffer at 100 ms intervals.
- **Serial via UNIX socket.** QEMU is started with
  `-serial unix:SOCK,server,nowait`; the harness connects as a client.
  This provides both boot log capture and REPL command injection.
- **Random ports.** Each QEMU instance gets randomly allocated host ports
  for API and metrics so suites can run in parallel without conflicts.
- **Evidence on failure.** Failed cases attach the serial log path and
  other captured artifacts to the result for triage.

## Suite map

| Suite | Build phases covered |
| --- | --- |
| smoke (built-in) | boot sanity |
| build-and-boot | 00-10, 35 |
| agent-core | 11-14, 26, 27 |
| providers | 15-22, 39, 91 |
| tools | 23-25, 92 |
| services-http | 28-31, 56-59 |
| provenance-security | 32-34, 48, 68-73, 87, 101 |
| storage | 64-67, 98 |
| logging-time | 60-63 |
| devices-power | 74-77, 96 |
| network-firewall | 79-81 |
| updates | 52, 82-86, 88 |
| performance | 45, 104 |
| e2e | 43, 44, 103 |

See `adr/001-harness-language.md` for the language choice rationale.

## Coverage checklist

The test automation system is built across 21 phases (T00–T20). Each row links
the implementation artefact to the phase that produced it. All phases are
complete for v1.0.

| Phase | Title | Key artefacts | Status |
| --- | --- | --- | --- |
| T00 | Harness scaffold | `tests/go.mod`, `tests/harness/`, `tests/reporters/`, `tests/cmd/run-suite/main.go` | ✓ |
| T01 | CI workflow | `.github/workflows/test.yml` (build-image, build-harness, suite matrix, aggregate jobs) | ✓ |
| T02 | Issue filing | `tests/tools/file-issue.sh` (create, dedup, recurrence comment, close-on-green) | ✓ |
| T03 | CI documentation | `tests/ci.md` | ✓ |
| T04 | build-and-boot suite | `tests/suites/build-and-boot/suite.go` (6 cases) | ✓ |
| T05 | agent-core suite | `tests/suites/agent-core/suite.go` (8 cases) | ✓ |
| T06 | providers suite | `tests/suites/providers/suite.go` (9 cases, SSE parser, model gating) | ✓ |
| T07 | tools suite | `tests/suites/tools/suite.go` (tool-list and invocation cases) | ✓ |
| T08 | services-http suite | `tests/suites/services-http/suite.go` (all gateway endpoints) | ✓ |
| T09 | provenance-security suite | `tests/suites/provenance-security/suite.go` (signing, secret scan) | ✓ |
| T10 | storage suite | `tests/suites/storage/suite.go` (data image, persistence) | ✓ |
| T11 | logging-time suite | `tests/suites/logging-time/suite.go` (structured logs, clock sync) | ✓ |
| T12 | devices-power suite | `tests/suites/devices-power/suite.go` (power-loss, device enumeration) | ✓ |
| T13 | network-firewall suite | `tests/suites/network-firewall/suite.go` (firewall policy, offline boot) | ✓ |
| T14 | updates suite | `tests/suites/updates/suite.go` (A/B slot switch, rollback) | ✓ |
| T15 | performance suite | `tests/suites/performance/suite.go` (boot time, chat latency) | ✓ |
| T16 | e2e suite | `tests/suites/e2e/suite.go` (full chat round-trip, tool call) | ✓ |
| T17 | Reporting | `tests/tools/merge-reports.sh`, `tests/reporting.md` (merged-report.json, badge, trend, HTML) | ✓ |
| T18 | Issue governance | `tests/tools/flake-detect.sh`, `tests/tools/quarantine.json`, `tests/issue-automation.md` | ✓ |
| T19 | Local runner | `scripts/test.sh`, `tests/tools/install-hook.sh`, `tests/local.md` | ✓ |
| T20 | Validation | This checklist, `tests/README.md` (final sign-off) | ✓ |

### Case count by suite

| Suite | Cases |
| --- | --- |
| build-and-boot | 6 |
| agent-core | 8 |
| providers | 9 |
| tools | varies (tool count) |
| services-http | 11 (one per endpoint + POST /chat) |
| provenance-security | 5+ |
| storage | 4+ |
| logging-time | 4+ |
| devices-power | 4+ |
| network-firewall | 3+ |
| updates | 4+ |
| performance | 3+ |
| e2e | 3+ |
| **Total** | **≥ 70** |

### Tooling checklist

- [x] `go vet ./...` passes on the `tests/` module
- [x] All suites compile with `go build ./...`
- [x] `tests/.gitignore` anchors `/run-suite` (binary only, not source dir)
- [x] No external Go dependencies (`tests/go.mod` stdlib only)
- [x] `file-issue.sh` creates labels idempotently via `--force`
- [x] `merge-reports.sh` exits 1 when any suite failed
- [x] `flake-detect.sh` supports `--dry-run`
- [x] `scripts/test.sh` checks prereqs before building
- [x] Pre-push hook is opt-in (`install-hook.sh`), not installed by default
- [x] All shell scripts pass `shellcheck` (set -euo pipefail, quoted expansions)
- [x] CI sets `fail-fast: false` so all suites run regardless of individual failures
- [x] Remote provider cases skip (not fail) when API keys are absent
- [x] QEMU `NoNetwork` mode used for offline-boot tests
- [x] Evidence bundles redact API keys and Bearer tokens before writing
