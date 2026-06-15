# NuraOS CI Integration Tests

This document describes the GitHub Actions workflow defined in
`.github/workflows/test.yml` that runs the NuraOS integration test suite.

## Triggers

| Event | Behaviour |
| --- | --- |
| `push` to `main` | Full suite run; issues are filed for failures |
| `pull_request` to `main` | Full suite run; issue filer runs in dry-run mode (annotates log only) |
| Scheduled (`0 2 * * *`) | Nightly run from `main`; issues filed |

## Jobs

### `build-image` -- Build or restore the NuraOS image

Produces `image/out/{bzImage,initramfs.cpio.gz,data.img,manifest.json}`.
The cache key is a SHA-256 fingerprint of all source files under `scripts/`,
`agent/`, `services/`, and `rootfs/`. A cache hit skips the build entirely
(typically 15-40 min saved per run).

### `build-harness` -- Compile the Go test harness

Builds `tests/run-suite` with `go build -trimpath`. The binary is uploaded as
a workflow artifact (`run-suite`) and downloaded by each suite job so the
compilation happens once regardless of matrix size.

### `suite` -- Suite matrix (parallel)

Runs all 13 subsystem suites in parallel as a job matrix with
`fail-fast: false` so a broken suite does not cancel the others:

| Suite | Description |
| --- | --- |
| `build-and-boot` | Kernel build, image assembly, QEMU boot, readiness |
| `agent-core` | Rust agent: config, logging, serial REPL, context |
| `providers` | Local llama.cpp + remote Anthropic / OpenAI conformance |
| `tools` | Built-in tool calls (filesystem, code exec, search) |
| `services-http` | Gateway HTTP API contract |
| `provenance-security` | SBOM, signatures, secret-free builds |
| `storage` | /data persistence across reboots |
| `logging-time` | Structured logs, correct timezone, NTP |
| `devices-power` | Hardware detection, ACPI power-off |
| `network-firewall` | Egress rules, DNS, firewall policy |
| `updates` | OTA update path, rollback |
| `performance` | Boot-to-ready latency, memory footprint |
| `e2e` | Multi-turn AI session from prompt to response |

Each suite job:
1. Restores the cached `image/out/`.
2. Downloads the `run-suite` harness binary.
3. Installs QEMU (`qemu-system-x86`).
4. Runs `tests/run-suite <suite>` with `NURA_REPO_ROOT` set.
5. Uploads `tests/reports/<suite>/` as a workflow artifact (30-day retention).
6. Writes a pass/fail/skip table to the GitHub job summary.
7. Files or comments on GitHub issues for failures (main/nightly only).
8. Closes resolved issues when a previously failing case goes green.

## Secret gating for remote providers

Remote-provider test cases read `ANTHROPIC_API_KEY` and `OPENAI_API_KEY` from
the environment. When a secret is absent (empty string), the suite function must
**skip** the affected cases (return `StatusSkip`, not `StatusFail`). This keeps
PR runs green on forks that do not have access to org secrets.

Suite implementations must check:
```go
if os.Getenv("ANTHROPIC_API_KEY") == "" {
    return harness.Result{Suite: suite, Case: "anthropic-chat",
        Status: harness.StatusSkip, Message: "ANTHROPIC_API_KEY not set"}
}
```

## Issue filing

On `main` and nightly runs, `tests/tools/file-issue.sh` is called once per
failing result JSON. See [issue-automation.md](issue-automation.md) for the
deduplication logic, labels, and required token scopes.

On PR runs, `--dry-run` is passed so the log shows what would be filed without
creating real issues. This prevents noise from transient failures on branches
that have not been merged yet.

The `GITHUB_TOKEN` (with `issues: write` permission granted by the workflow)
is sufficient -- no PAT is needed.

## Concurrency

```yaml
concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true
```

A new push to the same branch cancels the previous in-flight run. This ensures
that only the latest commit is tested when developers push rapidly, and avoids
wasting runner minutes on stale builds.

## Artifacts

Each suite job uploads `tests/reports/<suite>/` which contains:
- `<suite>-junit.xml` -- JUnit XML (compatible with GitHub test summary and most CI dashboards)
- `<suite>-report.json` -- Full structured report (RunReport with RunID, CommitSHA, Results)
- `<case>-evidence/` -- Per-failure evidence bundle: `serial.log`, `metrics.txt`, `config.json`

Artifacts are retained for 30 days (1 day for the harness binary).

## Local usage

To run a single suite locally against a booted image:

```sh
# Build the harness binary.
cd tests && go build -o run-suite ./cmd/run-suite

# Point at the repo root and run a suite.
NURA_REPO_ROOT=/path/to/nuraos tests/run-suite smoke
NURA_REPO_ROOT=/path/to/nuraos tests/run-suite build-and-boot
```

Reports are written to `tests/reports/<suite>/` relative to `NURA_REPO_ROOT`.
