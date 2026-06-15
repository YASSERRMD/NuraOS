# Running NuraOS tests locally

`scripts/test.sh` is the local counterpart to the CI test matrix. It builds
the test harness, verifies (or builds) the NuraOS image, runs the requested
suites, and prints a consolidated summary.

## Prerequisites

| Tool | Minimum version | Notes |
| --- | --- | --- |
| `go` | 1.23 | Build the test harness |
| `qemu-system-x86_64` | 7.x | Boot NuraOS under QEMU |
| `jq` | 1.6 | Parse suite reports |

Optional (for remote-provider test cases):
```
export ANTHROPIC_API_KEY=sk-ant-api...
export OPENAI_API_KEY=sk-proj-...
```

## Quick start

```sh
# Run all suites (will build the image on first run):
./scripts/test.sh --build-image

# Run a single suite without rebuilding the image:
./scripts/test.sh agent-core

# Run two suites and generate a merged HTML report:
./scripts/test.sh --merge services-http providers

# Force-rebuild the harness binary (after changing test code):
./scripts/test.sh --rebuild build-and-boot
```

## All options

```
./scripts/test.sh [OPTIONS] [SUITE...]

Arguments:
  SUITE             One or more suite names (default: all 13 suites).

Options:
  --rebuild         Recompile tests/run-suite even if the binary exists.
  --build-image     (Re)build the NuraOS image before running tests.
                    Without this flag, the script checks whether
                    image/out/ already contains the four expected
                    artifacts and skips the build if they are present.
  --report-dir DIR  Write per-suite JSON reports here
                    (default: tests/reports).
  --merge           Run tests/tools/merge-reports.sh after all suites
                    finish to produce merged-report.json, summary.html,
                    badge.json, and trend.jsonl.
  --no-color        Disable ANSI colour in output (useful in scripts).
  --help            Show usage.
```

## Suite names

```
build-and-boot     providers          devices-power
agent-core         tools              network-firewall
services-http      storage            updates
provenance-security logging-time      performance
e2e
```

## How the runner works

1. **Prerequisite check** - `go`, `qemu-system-x86_64`, and `jq` must be on
   `PATH`. The script exits immediately if any are missing.
2. **Image check** - If all four image artefacts
   (`bzImage`, `initramfs.cpio.gz`, `nura.img`, `data.img`) exist in
   `image/out/`, the build is skipped. Pass `--build-image` to rebuild.
3. **Harness build** - `go build -trimpath -o tests/run-suite ./cmd/run-suite`
   is run once. Subsequent invocations reuse the cached binary unless
   `--rebuild` is passed.
4. **Suite execution** - Each suite is run via
   `tests/run-suite <suite>` in order. Suites are sequential locally (unlike
   CI which runs them in parallel). Each suite boots its own QEMU instance.
5. **Report accumulation** - Each suite writes its results to
   `tests/reports/<suite>/<suite>-report.json`. Pass `--merge` to aggregate
   all suites into `tests/reports/merged-report.json`.

## Report files

| File | Description |
| --- | --- |
| `tests/reports/<suite>/<suite>-report.json` | Per-suite result (RunReport JSON) |
| `tests/reports/merged-report.json` | All suites merged (requires `--merge`) |
| `tests/reports/summary.html` | Human-readable HTML summary |
| `tests/reports/badge.json` | Badge data for README integration |
| `tests/reports/trend.jsonl` | Append-only run history |

The `tests/reports/` directory is gitignored; report files are never committed.

## Pre-push hook (optional)

The pre-push hook runs the fast `smoke` suite before every `git push`,
preventing obviously broken code from reaching the remote.

```sh
# Install:
tests/tools/install-hook.sh

# Uninstall:
tests/tools/install-hook.sh --uninstall

# Bypass for a specific push:
git push --no-verify
```

The hook is not installed by default. Installation is per-developer and
per-clone.

## Selecting test cases that need API keys

Cases that call remote AI providers check whether the relevant environment
variable is set and skip (not fail) when absent. To exercise them locally:

```sh
export ANTHROPIC_API_KEY=sk-ant-api...
./scripts/test.sh providers
```

The harness will skip remote-provider cases and run everything else when the
key is absent. No special flag is required.

## Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| `qemu-system-x86_64: command not found` | QEMU not installed | `brew install qemu` (macOS) or `apt install qemu-system-x86` |
| Suite hangs indefinitely | QEMU failed to boot | Check `tests/reports/<suite>/serial.log` |
| `boot-ready` case fails | NuraOS image not built | Run with `--build-image` |
| `no report generated` | Harness crashed before writing output | Run with `--rebuild` to pick up any harness changes |
| Remote provider cases skip | `ANTHROPIC_API_KEY` not set | Export the key in your shell |
