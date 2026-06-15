#!/usr/bin/env bash
# smoke-test.sh -- End-to-end smoke matrix against a running nura-gateway.
#
# Usage:
#   ./scripts/smoke-test.sh [OPTIONS]
#
# Options:
#   --base-url URL   Gateway base URL (default: http://127.0.0.1:8080)
#   --token TOKEN    Bearer token for auth-enabled gateways (optional)
#   --verbose        Print full response bodies
#   --fail-fast      Exit on first failure
#
# The script exercises every gateway endpoint and prints a pass/fail matrix.
# It requires curl. The gateway must be running and healthy before invocation.
#
# Exit code: 0 if all checks pass, 1 if any fail.

set -euo pipefail

BASE_URL="http://127.0.0.1:8080"
TOKEN=""
VERBOSE=0
FAIL_FAST=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --base-url)  BASE_URL="$2";  shift 2 ;;
        --token)     TOKEN="$2";     shift 2 ;;
        --verbose)   VERBOSE=1;      shift ;;
        --fail-fast) FAIL_FAST=1;    shift ;;
        --help|-h)
            grep '^#' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) echo "[smoke] unknown argument: $1" >&2; exit 1 ;;
    esac
done

PASS=0
FAIL=0

auth_args=()
if [ -n "${TOKEN}" ]; then
    auth_args=(-H "Authorization: Bearer ${TOKEN}")
fi

check() {
    local label="$1"
    local method="$2"
    local path="$3"
    local want_code="$4"
    local want_key="${5:-}"

    local url="${BASE_URL}${path}"
    local body
    local code

    if [ "${method}" = "POST" ]; then
        body=$(curl -s -o /tmp/smoke_body.txt -w "%{http_code}" \
            -X POST "${auth_args[@]}" \
            -H "Content-Type: application/json" \
            -d '{"messages":[{"role":"user","parts":[{"type":"text","text":"hi"}]}]}' \
            "${url}" 2>/dev/null) || true
    else
        body=$(curl -s -o /tmp/smoke_body.txt -w "%{http_code}" \
            "${auth_args[@]}" "${url}" 2>/dev/null) || true
    fi
    code="${body}"

    local resp=""
    resp=$(cat /tmp/smoke_body.txt 2>/dev/null || echo "")

    local status="PASS"
    local note=""

    if [ "${code}" != "${want_code}" ]; then
        status="FAIL"
        note="expected HTTP ${want_code}, got ${code}"
    elif [ -n "${want_key}" ]; then
        if ! echo "${resp}" | grep -q "\"${want_key}\""; then
            status="FAIL"
            note="key '${want_key}' not found in response"
        fi
    fi

    if [ "${status}" = "PASS" ]; then
        PASS=$(( PASS + 1 ))
        printf '  [PASS]  %-30s  %s %s\n' "${label}" "${method}" "${path}"
    else
        FAIL=$(( FAIL + 1 ))
        printf '  [FAIL]  %-30s  %s %s  -- %s\n' "${label}" "${method}" "${path}" "${note}"
        if [ "${VERBOSE}" -eq 1 ]; then
            echo "          Response: ${resp}"
        fi
        if [ "${FAIL_FAST}" -eq 1 ]; then
            echo "[smoke] aborting on first failure"
            exit 1
        fi
    fi
}

echo "[smoke] target: ${BASE_URL}"
echo "[smoke] ---- smoke matrix ----"

check "healthz"          GET  /healthz         200  "status"
check "version"          GET  /version          200  "version"
check "config"           GET  /config           200  "gateway"
check "tools"            GET  /tools            200  "tools"
check "metrics"          GET  /metrics          200  ""
check "status"           GET  /status           200  "overall"
check "models"           GET  /models           200  "available"
check "update/status"    GET  /update/status    200  "active_slot"
check "telemetry/status" GET  /telemetry/status 200  "telemetry"
check "board"            GET  /board            200  "board"
check "chat (empty msg)" POST /chat             400  "error"

echo ""
echo "[smoke] results: ${PASS} passed, ${FAIL} failed"

if [ "${FAIL}" -gt 0 ]; then
    exit 1
fi
