#!/usr/bin/env bash
# file-issue.sh -- Create or update GitHub issues for failing NuraOS test cases.
#
# For a failing result JSON, this script either creates a new issue or appends
# an occurrence comment to an existing open issue, deduplicated by the stable
# failure_signature embedded in the result. When a case recovers, it can
# comment and optionally auto-close the matching issue.
#
# Usage:
#   file-issue.sh [OPTIONS] <result-json-path>
#
# Options:
#   --dry-run         Print gh commands without executing; safe for local dev
#   --close-on-green  Handle a passing result: comment and optionally close
#   --auto-close      With --close-on-green: auto-close the issue on recovery
#   --help            Show this message
#
# Environment:
#   NURA_RUN_URL   Optional URL of the CI run, embedded in issue body and comments
#
# Required tools: gh (GitHub CLI), jq
# Token scopes: issues:write  (create, comment, close)

set -euo pipefail

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
DRY_RUN=0
CLOSE_ON_GREEN=0
AUTO_CLOSE=0
RESULT_FILE=""

usage() {
    grep '^#' "$0" | sed 's/^# \{0,1\}//'
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run)        DRY_RUN=1 ;;
        --close-on-green) CLOSE_ON_GREEN=1 ;;
        --auto-close)     AUTO_CLOSE=1 ;;
        --help|-h)        usage; exit 0 ;;
        --*)              echo "[file-issue] unknown option: $1" >&2; exit 1 ;;
        *)                RESULT_FILE="$1" ;;
    esac
    shift
done

if [[ -z "${RESULT_FILE}" ]]; then
    echo "[file-issue] error: result JSON path is required" >&2
    usage >&2
    exit 1
fi

if [[ ! -f "${RESULT_FILE}" ]]; then
    echo "[file-issue] error: file not found: ${RESULT_FILE}" >&2
    exit 1
fi

# ---------------------------------------------------------------------------
# Tool checks
# ---------------------------------------------------------------------------
check_tools() {
    local missing=0
    for cmd in gh jq; do
        if ! command -v "${cmd}" >/dev/null 2>&1; then
            echo "[file-issue] error: required tool not found: ${cmd}" >&2
            missing=1
        fi
    done
    [[ "${missing}" -eq 0 ]] || exit 1
}

# jq is always required (to parse the result JSON).
command -v jq >/dev/null 2>&1 || { echo "[file-issue] error: jq is required" >&2; exit 1; }
# gh is required unless --dry-run.
[[ "${DRY_RUN}" -eq 1 ]] || check_tools

# ---------------------------------------------------------------------------
# Extract fields from the result JSON
# ---------------------------------------------------------------------------
SUITE=$(jq -r '.suite     // ""' "${RESULT_FILE}")
CASE=$(jq  -r '.case      // ""' "${RESULT_FILE}")
STATUS=$(jq -r '.status   // ""' "${RESULT_FILE}")
SIG=$(jq    -r '.failure_signature // ""' "${RESULT_FILE}")
MESSAGE=$(jq -r '.message // ""' "${RESULT_FILE}")
COMMIT=$(jq  -r '.commit_sha // "unknown"' "${RESULT_FILE}")
RUN_ID=$(jq  -r '.run_id  // "unknown"' "${RESULT_FILE}")
BUNDLE=$(jq  -r '.evidence.bundle_dir // ""' "${RESULT_FILE}")

RUN_URL="${NURA_RUN_URL:-}"
DATE=$(date -u "+%Y-%m-%d %H:%M UTC")

if [[ -z "${SUITE}" || -z "${CASE}" || -z "${STATUS}" ]]; then
    echo "[file-issue] error: result JSON is missing required fields (suite, case, status)" >&2
    exit 1
fi

# ---------------------------------------------------------------------------
# gh wrapper: respects DRY_RUN (also used by later commits)
# ---------------------------------------------------------------------------
gh_exec() {
    if [[ "${DRY_RUN}" -eq 1 ]]; then
        echo "[dry-run] gh $*" >&2
        return 0
    fi
    gh "$@"
}

# ---------------------------------------------------------------------------
# Dedup search: find the first open issue whose body contains the stable
# failure_signature marker <!-- nuraos-sig: SIG -->.
# Returns the issue number on stdout, or empty string if none found.
# ---------------------------------------------------------------------------
find_open_issue_by_sig() {
    local sig="$1"
    if [[ -z "${sig}" ]]; then
        echo ""
        return
    fi
    if [[ "${DRY_RUN}" -eq 1 ]]; then
        echo "[dry-run] gh issue list --search \"nuraos-sig: ${sig} in:body\" --state open ..." >&2
        echo ""
        return
    fi
    gh issue list \
        --search "\"nuraos-sig: ${sig}\" in:body" \
        --state open \
        --json number \
        --limit 5 \
        2>/dev/null \
        | jq -r '.[0].number // ""'
}

# ---------------------------------------------------------------------------
# Title search: find an open issue by the standard title pattern.
# Used by --close-on-green where no failure_signature exists in the result.
# ---------------------------------------------------------------------------
find_open_issue_by_title() {
    local suite="$1"
    local case_="$2"
    local title="[${suite}] ${case_} failing"
    if [[ "${DRY_RUN}" -eq 1 ]]; then
        echo "[dry-run] gh issue list --search \"${title} in:title\" --state open ..." >&2
        echo ""
        return
    fi
    gh issue list \
        --search "\"${title}\" in:title" \
        --state open \
        --json number \
        --limit 5 \
        2>/dev/null \
        | jq -r '.[0].number // ""'
}

# ---------------------------------------------------------------------------
# Issue body for a new failure
# ---------------------------------------------------------------------------
failure_body() {
    local run_line=""
    [[ -n "${RUN_URL}" ]] && run_line="**CI run:** ${RUN_URL}"
    local bundle_line=""
    [[ -n "${BUNDLE}" ]] && bundle_line="**Evidence bundle:** see CI artifacts (path: \`${BUNDLE}\`)"

    printf '%s\n' \
        "**Test suite:** \`${SUITE}\`" \
        "**Test case:** \`${CASE}\`" \
        "**Status:** FAIL" \
        "**Error:** ${MESSAGE}" \
        "" \
        "**Commit:** \`${COMMIT}\`" \
        "**Run ID:** \`${RUN_ID}\`" \
        "${run_line}" \
        "${bundle_line}" \
        "" \
        "---" \
        "" \
        "_First seen: ${DATE}_" \
        "" \
        "<!-- nuraos-sig: ${SIG} -->"
}

# ---------------------------------------------------------------------------
# Ensure labels exist (idempotent -- ignore error if already present)
# ---------------------------------------------------------------------------
ensure_label() {
    local name="$1" color="$2" desc="$3"
    gh_exec label create "${name}" \
        --color "${color}" \
        --description "${desc}" \
        --force 2>/dev/null || true
}

# ---------------------------------------------------------------------------
# Create a new issue for this failure
# ---------------------------------------------------------------------------
create_issue() {
    local title="[${SUITE}] ${CASE} failing"
    local body
    body=$(failure_body)

    if [[ "${DRY_RUN}" -eq 1 ]]; then
        echo "[dry-run] gh issue create --title \"${title}\" --label test-failure,suite:${SUITE} --body <failure-body>"
        echo "[dry-run] body would contain: <!-- nuraos-sig: ${SIG} -->"
        return
    fi

    ensure_label "test-failure"  "d73a4a" "Automated test failure"
    ensure_label "suite:${SUITE}" "0075ca" "Suite: ${SUITE}"

    local url
    url=$(gh issue create \
        --title "${title}" \
        --label "test-failure,suite:${SUITE}" \
        --body "${body}")
    echo "[file-issue] created issue: ${url}"
}
