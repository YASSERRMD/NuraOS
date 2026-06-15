#!/usr/bin/env bash
# test.sh -- Local NuraOS integration test runner.
#
# Builds the test harness (if necessary), then runs one or more suites against
# a freshly booted NuraOS QEMU instance. Outputs pass/fail per case and an
# overall exit code matching the CI gate.
#
# Usage:
#   ./scripts/test.sh [OPTIONS] [SUITE...]
#
# Arguments:
#   SUITE    One or more suite names to run (default: all suites).
#            Valid names: build-and-boot agent-core providers tools
#                         services-http provenance-security storage
#                         logging-time devices-power network-firewall
#                         updates performance e2e
#
# Options:
#   --rebuild         Force rebuild of the run-suite binary even if it exists.
#   --build-image     Force rebuild of the NuraOS image (skipped by default
#                     when image/out/ already contains the expected artifacts).
#   --report-dir DIR  Write suite reports here (default: tests/reports).
#   --merge           Run merge-reports.sh after all suites finish.
#   --no-color        Disable ANSI colour output.
#   --help            Show this message.
#
# Environment variables forwarded to the harness:
#   NURA_REPO_ROOT       Defaults to the repository root (auto-detected).
#   NURA_RUN_URL         Human-readable URL stamped into failure reports.
#   ANTHROPIC_API_KEY    Enables remote Anthropic provider test cases.
#   OPENAI_API_KEY       Enables OpenAI-compatible provider test cases.
#
# Prerequisites: go, qemu-system-x86_64, jq

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
TESTS_DIR="${REPO_ROOT}/tests"
HARNESS_BIN="${TESTS_DIR}/run-suite"
REPORT_DIR="${TESTS_DIR}/reports"
REBUILD=0
BUILD_IMAGE=0
MERGE=0
COLOR=1

ALL_SUITES=(
    build-and-boot
    agent-core
    providers
    tools
    services-http
    provenance-security
    storage
    logging-time
    devices-power
    network-firewall
    updates
    performance
    e2e
)

usage() { grep '^#' "$0" | sed 's/^# \{0,1\}//'; }

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
SUITES=()
while [[ $# -gt 0 ]]; do
    case "$1" in
        --rebuild)      REBUILD=1 ;;
        --build-image)  BUILD_IMAGE=1 ;;
        --report-dir)   shift; REPORT_DIR="$1" ;;
        --merge)        MERGE=1 ;;
        --no-color)     COLOR=0 ;;
        --help|-h)      usage; exit 0 ;;
        -*)             echo "[test] unknown option: $1" >&2; exit 1 ;;
        *)              SUITES+=("$1") ;;
    esac
    shift
done

[[ ${#SUITES[@]} -eq 0 ]] && SUITES=("${ALL_SUITES[@]}")

# ---------------------------------------------------------------------------
# Colour helpers
# ---------------------------------------------------------------------------
RED=""
GREEN=""
YELLOW=""
RESET=""
if [[ "${COLOR}" -eq 1 && -t 1 ]]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    RESET='\033[0m'
fi
pass() { printf "${GREEN}[PASS]${RESET} %s\n" "$*"; }
fail() { printf "${RED}[FAIL]${RESET} %s\n" "$*"; }
info() { printf "${YELLOW}[info]${RESET} %s\n" "$*"; }

# ---------------------------------------------------------------------------
# Prerequisite check
# ---------------------------------------------------------------------------
check_prereq() {
    command -v "$1" >/dev/null 2>&1 || {
        echo "[test] required tool not found: $1" >&2; exit 1
    }
}
check_prereq go
check_prereq qemu-system-x86_64
check_prereq jq

# ---------------------------------------------------------------------------
# Build the NuraOS image (optional)
# ---------------------------------------------------------------------------
IMAGE_ARTIFACTS=(
    "${REPO_ROOT}/image/out/bzImage"
    "${REPO_ROOT}/image/out/initramfs.cpio.gz"
    "${REPO_ROOT}/image/out/nura.img"
    "${REPO_ROOT}/image/out/data.img"
)

need_image=0
if [[ "${BUILD_IMAGE}" -eq 1 ]]; then
    need_image=1
else
    for art in "${IMAGE_ARTIFACTS[@]}"; do
        [[ -f "${art}" ]] || { need_image=1; break; }
    done
fi

if [[ "${need_image}" -eq 1 ]]; then
    info "Building NuraOS image (this takes a while) ..."
    "${REPO_ROOT}/scripts/build-image.sh"
else
    info "NuraOS image found in image/out/; skipping rebuild (use --build-image to force)"
fi

# ---------------------------------------------------------------------------
# Build the test harness binary
# ---------------------------------------------------------------------------
if [[ "${REBUILD}" -eq 1 || ! -x "${HARNESS_BIN}" ]]; then
    info "Building tests/run-suite ..."
    (cd "${TESTS_DIR}" && go build -trimpath -o run-suite ./cmd/run-suite)
    info "Harness built: ${HARNESS_BIN}"
else
    info "Harness binary already exists; skipping rebuild (use --rebuild to force)"
fi

# ---------------------------------------------------------------------------
# Run suites
# ---------------------------------------------------------------------------
export NURA_REPO_ROOT="${NURA_REPO_ROOT:-${REPO_ROOT}}"
export NURA_RUN_URL="${NURA_RUN_URL:-local}"

mkdir -p "${REPORT_DIR}"

PASS_SUITES=()
FAIL_SUITES=()
TOTAL_PASS=0
TOTAL_FAIL=0
TOTAL_SKIP=0

for suite in "${SUITES[@]}"; do
    info "Running suite: ${suite}"
    if REPORT_DIR="${REPORT_DIR}" "${HARNESS_BIN}" "${suite}"; then
        PASS_SUITES+=("${suite}")
    else
        FAIL_SUITES+=("${suite}")
    fi

    report="${REPORT_DIR}/${suite}/${suite}-report.json"
    if [[ -f "${report}" ]]; then
        p=$(jq '[.results[] | select(.status=="pass")] | length' "${report}" 2>/dev/null || echo 0)
        f=$(jq '[.results[] | select(.status=="fail")] | length' "${report}" 2>/dev/null || echo 0)
        s=$(jq '[.results[] | select(.status=="skip")] | length' "${report}" 2>/dev/null || echo 0)
        TOTAL_PASS=$(( TOTAL_PASS + p ))
        TOTAL_FAIL=$(( TOTAL_FAIL + f ))
        TOTAL_SKIP=$(( TOTAL_SKIP + s ))

        if [[ "${f}" -gt 0 ]]; then
            fail "  ${suite}: ${p} pass / ${f} fail / ${s} skip"
            jq -r '.results[] | select(.status=="fail") |
                "    - \(.case): \(.message // "no message")"' "${report}" 2>/dev/null || true
        else
            pass "  ${suite}: ${p} pass / ${f} fail / ${s} skip"
        fi
    else
        fail "  ${suite}: no report generated"
    fi
done

# ---------------------------------------------------------------------------
# Merge reports (optional)
# ---------------------------------------------------------------------------
if [[ "${MERGE}" -eq 1 ]]; then
    info "Merging reports ..."
    chmod +x "${TESTS_DIR}/tools/merge-reports.sh"
    "${TESTS_DIR}/tools/merge-reports.sh" \
        --report-dir "${REPORT_DIR}" \
        --commit "$(git -C "${REPO_ROOT}" rev-parse HEAD 2>/dev/null || echo local)" \
        || true
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  NuraOS Test Summary"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Cases:  ${TOTAL_PASS} pass  ${TOTAL_FAIL} fail  ${TOTAL_SKIP} skip"
echo "  Suites: ${#PASS_SUITES[@]} pass  ${#FAIL_SUITES[@]} fail"
if [[ ${#FAIL_SUITES[@]} -gt 0 ]]; then
    echo ""
    echo "  Failed suites:"
    for s in "${FAIL_SUITES[@]}"; do
        echo "    - ${s}"
    done
fi
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

[[ ${#FAIL_SUITES[@]} -eq 0 ]]
