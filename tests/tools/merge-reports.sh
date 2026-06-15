#!/usr/bin/env bash
# merge-reports.sh -- Merge all suite JSON reports into one run report.
#
# Reads every <report-dir>/<suite>/<suite>-report.json, aggregates totals,
# and writes a single merged-report.json + job-summary.md to <report-dir>/.
#
# Usage:
#   tests/tools/merge-reports.sh [--report-dir DIR] [--commit SHA] [--run-url URL]
#
# Options:
#   --report-dir DIR   path to the reports root (default: tests/reports)
#   --commit SHA       git commit SHA stamped into the merged report
#   --run-url URL      CI run URL linked in the summary
#   --badge-out FILE   write badge JSON to FILE (default: <report-dir>/badge.json)
#   --trend-out FILE   append one trend line to FILE (default: tests/reports/trend.jsonl)
#   --html-out FILE    write HTML summary to FILE (default: <report-dir>/summary.html)
#   --help             show this message

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
REPORT_DIR="${REPO_ROOT}/tests/reports"
COMMIT_SHA=""
RUN_URL=""
BADGE_OUT=""
TREND_OUT=""
HTML_OUT=""

usage() { grep '^#' "$0" | sed 's/^# \{0,1\}//'; }

while [[ $# -gt 0 ]]; do
    case "$1" in
        --report-dir) shift; REPORT_DIR="$1" ;;
        --commit)     shift; COMMIT_SHA="$1" ;;
        --run-url)    shift; RUN_URL="$1" ;;
        --badge-out)  shift; BADGE_OUT="$1" ;;
        --trend-out)  shift; TREND_OUT="$1" ;;
        --html-out)   shift; HTML_OUT="$1" ;;
        --help|-h)    usage; exit 0 ;;
        *) echo "[merge-reports] unknown option: $1" >&2; exit 1 ;;
    esac
    shift
done

BADGE_OUT="${BADGE_OUT:-${REPORT_DIR}/badge.json}"
TREND_OUT="${TREND_OUT:-${REPORT_DIR}/trend.jsonl}"
HTML_OUT="${HTML_OUT:-${REPORT_DIR}/summary.html}"
MERGED_OUT="${REPORT_DIR}/merged-report.json"
SUMMARY_OUT="${REPORT_DIR}/job-summary.md"

command -v jq >/dev/null 2>&1 || { echo "[merge-reports] jq is required" >&2; exit 1; }

if [[ -z "${COMMIT_SHA}" ]]; then
    COMMIT_SHA=$(git -C "${REPO_ROOT}" rev-parse HEAD 2>/dev/null || echo "unknown")
fi
DATE=$(date -u "+%Y-%m-%dT%H:%M:%SZ")

# ---------------------------------------------------------------------------
# Collect all suite reports
# ---------------------------------------------------------------------------
collect_reports() {
    find "${REPORT_DIR}" -maxdepth 2 -name "*-report.json" | sort
}

# ---------------------------------------------------------------------------
# Merge into one JSON document
# ---------------------------------------------------------------------------
TOTAL_PASS=0; TOTAL_FAIL=0; TOTAL_SKIP=0
SUITE_SUMMARIES="[]"
ALL_RESULTS="[]"

while IFS= read -r report; do
    [[ -f "${report}" ]] || continue
    suite=$(jq -r '.suite // "unknown"' "${report}" 2>/dev/null) || continue

    pass=$(jq '[.results[] | select(.status=="pass")] | length' "${report}" 2>/dev/null || echo 0)
    fail=$(jq '[.results[] | select(.status=="fail")] | length' "${report}" 2>/dev/null || echo 0)
    skip=$(jq '[.results[] | select(.status=="skip")] | length' "${report}" 2>/dev/null || echo 0)

    TOTAL_PASS=$((TOTAL_PASS + pass))
    TOTAL_FAIL=$((TOTAL_FAIL + fail))
    TOTAL_SKIP=$((TOTAL_SKIP + skip))

    status="pass"
    [[ "${fail}" -gt 0 ]] && status="fail"

    SUITE_SUMMARIES=$(echo "${SUITE_SUMMARIES}" | jq \
        --arg suite "${suite}" --argjson pass "${pass}" \
        --argjson fail "${fail}" --argjson skip "${skip}" \
        --arg status "${status}" \
        '. + [{"suite": $suite, "status": $status, "pass": $pass, "fail": $fail, "skip": $skip}]')

    suite_results=$(jq '.results // []' "${report}" 2>/dev/null || echo "[]")
    ALL_RESULTS=$(echo "${ALL_RESULTS}" | jq --argjson r "${suite_results}" '. + $r')
done < <(collect_reports)

TOTAL=$((TOTAL_PASS + TOTAL_FAIL + TOTAL_SKIP))
OVERALL_STATUS="pass"
[[ "${TOTAL_FAIL}" -gt 0 ]] && OVERALL_STATUS="fail"

jq -n \
    --arg commit "${COMMIT_SHA}" \
    --arg date "${DATE}" \
    --arg run_url "${RUN_URL}" \
    --arg status "${OVERALL_STATUS}" \
    --argjson total "${TOTAL}" \
    --argjson pass "${TOTAL_PASS}" \
    --argjson fail "${TOTAL_FAIL}" \
    --argjson skip "${TOTAL_SKIP}" \
    --argjson suites "${SUITE_SUMMARIES}" \
    --argjson results "${ALL_RESULTS}" \
    '{
        commit: $commit, date: $date, run_url: $run_url,
        status: $status,
        totals: {total: $total, pass: $pass, fail: $fail, skip: $skip},
        suites: $suites,
        results: $results
    }' > "${MERGED_OUT}"

echo "[merge-reports] merged report -> ${MERGED_OUT}"

# ---------------------------------------------------------------------------
# Badge JSON
# ---------------------------------------------------------------------------
PASS_RATE=0
[[ "${TOTAL}" -gt 0 ]] && PASS_RATE=$(( (TOTAL_PASS * 100) / TOTAL ))

jq -n \
    --arg label "tests" \
    --arg status "${OVERALL_STATUS}" \
    --arg pass_rate "${PASS_RATE}%" \
    --arg commit "${COMMIT_SHA:0:8}" \
    '{label: $label, status: $status, pass_rate: $pass_rate, commit: $commit}' \
    > "${BADGE_OUT}"
echo "[merge-reports] badge -> ${BADGE_OUT}"

# ---------------------------------------------------------------------------
# Trend line (append-only)
# ---------------------------------------------------------------------------
TREND_LINE=$(jq -n \
    --arg date "${DATE}" \
    --arg commit "${COMMIT_SHA:0:8}" \
    --arg status "${OVERALL_STATUS}" \
    --argjson pass "${TOTAL_PASS}" \
    --argjson fail "${TOTAL_FAIL}" \
    --argjson skip "${TOTAL_SKIP}" \
    '{date: $date, commit: $commit, status: $status, pass: $pass, fail: $fail, skip: $skip}')
echo "${TREND_LINE}" >> "${TREND_OUT}"
echo "[merge-reports] trend -> ${TREND_OUT}"

# ---------------------------------------------------------------------------
# Job summary markdown
# ---------------------------------------------------------------------------
{
    echo "# NuraOS Test Run Summary"
    echo ""
    echo "**Date:** ${DATE}  **Commit:** \`${COMMIT_SHA:0:8}\`  **Status:** ${OVERALL_STATUS^^}"
    [[ -n "${RUN_URL}" ]] && echo "  **CI Run:** [link](${RUN_URL})"
    echo ""
    echo "| Total | Pass | Fail | Skip |"
    echo "|-------|------|------|------|"
    echo "| ${TOTAL} | ${TOTAL_PASS} | ${TOTAL_FAIL} | ${TOTAL_SKIP} |"
    echo ""
    echo "## Suite Results"
    echo ""
    echo "| Suite | Status | Pass | Fail | Skip |"
    echo "|-------|--------|------|------|------|"
    echo "${SUITE_SUMMARIES}" | jq -r '.[] | "| \(.suite) | \(.status) | \(.pass) | \(.fail) | \(.skip) |"'
} > "${SUMMARY_OUT}"

# Print to GitHub step summary if available.
if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
    cat "${SUMMARY_OUT}" >> "${GITHUB_STEP_SUMMARY}"
fi
echo "[merge-reports] summary -> ${SUMMARY_OUT}"

# ---------------------------------------------------------------------------
# HTML artifact
# ---------------------------------------------------------------------------
{
    echo "<!DOCTYPE html><html><head><meta charset=utf-8>"
    echo "<title>NuraOS Test Run ${COMMIT_SHA:0:8}</title>"
    echo "<style>body{font-family:sans-serif;max-width:900px;margin:2em auto}"
    echo "table{border-collapse:collapse;width:100%}"
    echo "th,td{border:1px solid #ddd;padding:8px;text-align:left}"
    echo "tr.fail{background:#fdd} tr.pass{background:#dfd} tr.skip{background:#ffd}"
    echo "</style></head><body>"
    echo "<h1>NuraOS Test Run</h1>"
    echo "<p><strong>Date:</strong> ${DATE} &nbsp; <strong>Commit:</strong> <code>${COMMIT_SHA:0:8}</code></p>"
    echo "<p><strong>Status:</strong> ${OVERALL_STATUS} &nbsp; Pass: ${TOTAL_PASS} / Fail: ${TOTAL_FAIL} / Skip: ${TOTAL_SKIP}</p>"
    echo "<h2>Suite Summary</h2><table>"
    echo "<tr><th>Suite</th><th>Status</th><th>Pass</th><th>Fail</th><th>Skip</th></tr>"
    echo "${SUITE_SUMMARIES}" | jq -r '.[] | "<tr class=\"\(.status)\"><td>\(.suite)</td><td>\(.status)</td><td>\(.pass)</td><td>\(.fail)</td><td>\(.skip)</td></tr>"'
    echo "</table></body></html>"
} > "${HTML_OUT}"
echo "[merge-reports] html -> ${HTML_OUT}"

echo "[merge-reports] done: ${TOTAL_PASS} pass / ${TOTAL_FAIL} fail / ${TOTAL_SKIP} skip (${OVERALL_STATUS})"
[[ "${OVERALL_STATUS}" == "fail" ]] && exit 1 || exit 0
