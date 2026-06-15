#!/usr/bin/env bash
# security-check.sh -- Verify the NuraOS security posture before release.
#
# Checks:
#   1. No secret patterns in tracked files (API keys, tokens, passwords)
#   2. secrets.toml is not tracked in git
#   3. gateway auth uses constant-time comparison (source check)
#   4. Security headers middleware is present
#   5. Rate-limit and concurrency middleware are present
#   6. Bearer auth middleware is present
#   7. /data/etc/secrets.toml mode is 0600 (when running inside the guest)
#
# Usage:
#   ./scripts/security-check.sh [--guest]
#
# With --guest, also checks live file permissions in /data/etc/.

set -euo pipefail

GUEST_MODE=false
for arg in "$@"; do
    case "${arg}" in
        --guest) GUEST_MODE=true ;;
        --help|-h)
            grep '^#' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
    esac
done

PASS=0
FAIL=0
WARN=0

ok()   { printf '[security-check] PASS  %s\n' "$*"; PASS=$((PASS + 1)); }
fail() { printf '[security-check] FAIL  %s\n' "$*" >&2; FAIL=$((FAIL + 1)); }
warn() { printf '[security-check] WARN  %s\n' "$*"; WARN=$((WARN + 1)); }

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# ---- Check 1: No hardcoded secret patterns in tracked files ----
secret_patterns=(
    'sk-ant-[a-zA-Z0-9_-]{20,}'
    'sk-[a-zA-Z0-9]{20,}'
    'AKIA[0-9A-Z]{16}'
    'gateway_token\s*=\s*"[^"]{8,}"'
    'password\s*=\s*"[^"]{6,}"'
    'secret\s*=\s*"[^"]{8,}"'
)

found_secrets=false
for pattern in "${secret_patterns[@]}"; do
    hits=$(git -C "${REPO_ROOT}" ls-files \
        | grep -v -E '(secrets\.toml|\.pem|\.key|\.crt|security-check\.sh|_test\.go|\.md$)' \
        | xargs grep -lE "${pattern}" 2>/dev/null || true)
    if [ -n "${hits}" ]; then
        fail "potential secret pattern '${pattern}' found in: ${hits}"
        found_secrets=true
    fi
done
if [ "${found_secrets}" = "false" ]; then
    ok "no hardcoded secret patterns found in tracked files"
fi

# ---- Check 2: secrets.toml is not tracked in git ----
if git -C "${REPO_ROOT}" ls-files --error-unmatch "secrets.toml" 2>/dev/null; then
    fail "secrets.toml is tracked in git -- remove it immediately"
elif git -C "${REPO_ROOT}" ls-files --error-unmatch "data/etc/secrets.toml" 2>/dev/null; then
    fail "data/etc/secrets.toml is tracked in git -- remove it immediately"
else
    ok "secrets.toml is not tracked in git"
fi

# ---- Check 3: constant-time auth comparison ----
auth_file="${REPO_ROOT}/services/cmd/gateway/auth.go"
if [ -f "${auth_file}" ]; then
    if grep -q 'subtle.ConstantTimeCompare' "${auth_file}"; then
        ok "bearer auth uses constant-time comparison"
    else
        fail "bearer auth does NOT use constant-time comparison (timing attack risk)"
    fi
else
    warn "auth.go not found; skipping constant-time check"
fi

# ---- Check 4: security headers middleware ----
ratelimit_file="${REPO_ROOT}/services/cmd/gateway/ratelimit.go"
if [ -f "${ratelimit_file}" ]; then
    if grep -q 'securityHeadersMiddleware' "${ratelimit_file}"; then
        ok "security headers middleware is present"
    else
        fail "securityHeadersMiddleware not found in ratelimit.go"
    fi
else
    warn "ratelimit.go not found"
fi

# ---- Check 5: rate-limit and concurrency middleware ----
if [ -f "${ratelimit_file}" ]; then
    if grep -q 'rateLimitMiddleware' "${ratelimit_file}" \
       && grep -q 'concurrencyMiddleware' "${ratelimit_file}"; then
        ok "rate-limit and concurrency middleware are present"
    else
        fail "rate-limit or concurrency middleware missing"
    fi
fi

# ---- Check 6: auth middleware wired in main ----
main_file="${REPO_ROOT}/services/cmd/gateway/main.go"
if [ -f "${main_file}" ]; then
    if grep -q 'bearerAuthMiddleware' "${main_file}"; then
        ok "bearer auth middleware is wired in main.go"
    else
        fail "bearerAuthMiddleware not wired in main.go"
    fi
fi

# ---- Check 7: ReadTimeout set on the HTTP server ----
if [ -f "${main_file}" ]; then
    if grep -q 'ReadTimeout' "${main_file}"; then
        ok "HTTP server has ReadTimeout configured"
    else
        fail "HTTP server missing ReadTimeout (slow-client attack risk)"
    fi
fi

# ---- Check 8: MaxBytesReader on chat handler ----
handler_file="${REPO_ROOT}/services/cmd/gateway/handler.go"
if [ -f "${handler_file}" ]; then
    if grep -q 'MaxBytesReader' "${handler_file}"; then
        ok "chat handler caps request body with MaxBytesReader"
    else
        fail "chat handler missing MaxBytesReader (large-body attack risk)"
    fi
fi

# ---- Check 9: live /data/etc/ permissions (guest mode only) ----
if [ "${GUEST_MODE}" = "true" ]; then
    if [ -f "/data/etc/secrets.toml" ]; then
        perms=$(stat -c '%a' /data/etc/secrets.toml 2>/dev/null || stat -f '%A' /data/etc/secrets.toml 2>/dev/null || echo "unknown")
        if [ "${perms}" = "600" ]; then
            ok "/data/etc/secrets.toml mode is 600"
        else
            fail "/data/etc/secrets.toml mode is ${perms} (want 600)"
        fi
    else
        warn "/data/etc/secrets.toml not present (ok for local-only mode)"
    fi
fi

# ---- Summary ----
echo ""
printf '[security-check] Results: %d passed, %d warnings, %d failed\n' "${PASS}" "${WARN}" "${FAIL}"
echo ""
if [ "${FAIL}" -gt 0 ]; then
    exit 1
fi
