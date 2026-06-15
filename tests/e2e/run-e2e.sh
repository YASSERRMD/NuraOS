#!/usr/bin/env bash
# run-e2e.sh -- End-to-end test suite that exercises the NuraOS gateway API
# running in a QEMU instance (or any host:port target).
#
# The gateway must already be reachable before this script is invoked.
# Use scripts/run-qemu.sh with --timeout 120 in a background process
# and wait for /healthz to return 200 before running this script.
#
# Usage:
#   ./tests/e2e/run-e2e.sh [--host HOST] [--port PORT] [--token TOKEN]
#
# Options:
#   --host HOST    Gateway host (default: 127.0.0.1)
#   --port PORT    Gateway port (default: 8080)
#   --token TOKEN  Bearer token (default: none)
#   --timeout N    Per-request timeout in seconds (default: 10)

set -euo pipefail

HOST="127.0.0.1"
PORT="8080"
TOKEN=""
TIMEOUT=10

while [[ $# -gt 0 ]]; do
    case "$1" in
        --host)    shift; HOST="$1" ;;
        --port)    shift; PORT="$1" ;;
        --token)   shift; TOKEN="$1" ;;
        --timeout) shift; TIMEOUT="$1" ;;
        --help|-h)
            grep '^#' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) echo "[e2e] unknown argument: $1" >&2; exit 1 ;;
    esac
    shift
done

BASE="http://${HOST}:${PORT}"
PASS=0
FAIL=0
START=$(date +%s)

log()  { printf '[e2e] %s\n' "$*"; }
ok()   { printf '[e2e] PASS  %s\n' "$*"; PASS=$((PASS + 1)); }
fail() { printf '[e2e] FAIL  %s\n' "$*" >&2; FAIL=$((FAIL + 1)); }

curl_get() {
    local path="$1"
    local args=(-s -o /dev/null -w "%{http_code}" --max-time "${TIMEOUT}")
    [ -n "${TOKEN}" ] && args+=(-H "Authorization: Bearer ${TOKEN}")
    curl "${args[@]}" "${BASE}${path}"
}

curl_body() {
    local path="$1"
    local args=(-s --max-time "${TIMEOUT}")
    [ -n "${TOKEN}" ] && args+=(-H "Authorization: Bearer ${TOKEN}")
    curl "${args[@]}" "${BASE}${path}"
}

curl_post() {
    local path="$1"
    local body="$2"
    local args=(-s -o /dev/null -w "%{http_code}" --max-time "${TIMEOUT}" \
        -X POST -H "Content-Type: application/json" -d "${body}")
    [ -n "${TOKEN}" ] && args+=(-H "Authorization: Bearer ${TOKEN}")
    curl "${args[@]}" "${BASE}${path}"
}

log "target: ${BASE}"
log "token:  ${TOKEN:+(set)}"
log ""

# ---- Test 1: /healthz returns 200 ----
log "1. GET /healthz"
code=$(curl_get /healthz)
if [ "${code}" = "200" ]; then
    ok "/healthz returned 200"
else
    fail "/healthz returned ${code} (want 200)"
fi

# ---- Test 2: /version returns 200 with JSON ----
log "2. GET /version"
code=$(curl_get /version)
body=$(curl_body /version)
if [ "${code}" = "200" ]; then
    ok "/version returned 200"
else
    fail "/version returned ${code} (want 200)"
fi
if echo "${body}" | grep -q '"version"'; then
    ok "/version body contains 'version' key"
else
    fail "/version body missing 'version' key; got: ${body}"
fi

# ---- Test 3: /metrics returns Prometheus text ----
log "3. GET /metrics"
code=$(curl_get /metrics)
body=$(curl_body /metrics)
if [ "${code}" = "200" ]; then
    ok "/metrics returned 200"
else
    fail "/metrics returned ${code} (want 200)"
fi
if echo "${body}" | grep -q "nura_gateway_requests_total"; then
    ok "/metrics contains nura_gateway_requests_total"
else
    fail "/metrics missing nura_gateway_requests_total; got: ${body:0:200}"
fi

# ---- Test 4: /healthz is exempt from auth (no token needed) ----
log "4. GET /healthz without auth (should always pass)"
code=$(curl -s -o /dev/null -w "%{http_code}" --max-time "${TIMEOUT}" "${BASE}/healthz")
if [ "${code}" = "200" ]; then
    ok "/healthz is auth-exempt (200 without token)"
else
    fail "/healthz returned ${code} without token (want 200)"
fi

# ---- Test 5: auth check (only meaningful when token is set) ----
log "5. Auth enforcement"
if [ -n "${TOKEN}" ]; then
    # Request without token should be 401.
    code=$(curl -s -o /dev/null -w "%{http_code}" --max-time "${TIMEOUT}" "${BASE}/version")
    if [ "${code}" = "401" ]; then
        ok "/version without token returns 401 when auth is enabled"
    else
        fail "/version without token returned ${code} (want 401 when auth is enabled)"
    fi
    # Request with correct token should be 200.
    code=$(curl -s -o /dev/null -w "%{http_code}" --max-time "${TIMEOUT}" \
        -H "Authorization: Bearer ${TOKEN}" "${BASE}/version")
    if [ "${code}" = "200" ]; then
        ok "/version with valid token returns 200"
    else
        fail "/version with valid token returned ${code} (want 200)"
    fi
else
    ok "auth skip: no token set (run with --token to test auth enforcement)"
fi

# ---- Test 6: POST /chat with valid body returns 200 or 502/503 ----
log "6. POST /chat"
CHAT_BODY='{"messages":[{"role":"user","content":"ping"}]}'
code=$(curl_post /chat "${CHAT_BODY}")
if [ "${code}" = "200" ]; then
    ok "POST /chat returned 200 (agent responded)"
elif [ "${code}" = "502" ] || [ "${code}" = "503" ]; then
    ok "POST /chat returned ${code} (agent not available -- expected in no-model boot)"
elif [ "${code}" = "401" ] && [ -z "${TOKEN}" ]; then
    ok "POST /chat returned 401 (auth enforced; expected when --token not set)"
else
    fail "POST /chat returned ${code} (want 200, 502, 503, or 401)"
fi

# ---- Test 7: POST /chat with missing body returns 400 ----
log "7. POST /chat with empty body"
code=$(curl_post /chat "")
if [ "${code}" = "400" ] || [ "${code}" = "422" ]; then
    ok "POST /chat with empty body returns ${code}"
elif [ "${code}" = "401" ] && [ -z "${TOKEN}" ]; then
    ok "POST /chat with empty body returns 401 (auth checked before body)"
else
    fail "POST /chat with empty body returned ${code} (want 400/422 or 401)"
fi

# ---- Test 8: GET /tools returns 200 ----
log "8. GET /tools"
code=$(curl_get /tools)
if [ "${code}" = "200" ]; then
    ok "GET /tools returned 200"
elif [ "${code}" = "401" ] && [ -z "${TOKEN}" ]; then
    ok "GET /tools returned 401 (auth enforced; expected when --token not set)"
else
    fail "GET /tools returned ${code} (want 200 or 401)"
fi

# ---- Summary ----
END=$(date +%s)
ELAPSED=$((END - START))
log ""
log "Results: ${PASS} passed, ${FAIL} failed (${ELAPSED}s)"
log ""

if [ "${FAIL}" -gt 0 ]; then
    exit 1
fi
