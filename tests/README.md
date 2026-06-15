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
