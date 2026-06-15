#!/usr/bin/env bash
# verify-repro.sh -- Verify that the build is reproducible and lockfiles are
# consistent with their manifests.
#
# Checks:
#   1. Cargo.lock is tracked by git (not gitignored).
#   2. Cargo.lock is consistent with Cargo.toml (cargo metadata --locked).
#   3. Go module has no external dependencies that would require go.sum.
#   4. VERSIONS.env contains all required version pins.
#   5. No secrets.toml committed.
#
# Exit code: 0 = all checks passed; non-zero = at least one failed.
#
# Usage:
#   ./scripts/verify-repro.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

PASS=0
FAIL=0

ok()   { printf '[repro] OK    %s\n' "$*"; PASS=$((PASS + 1)); }
fail() { printf '[repro] FAIL  %s\n' "$*" >&2; FAIL=$((FAIL + 1)); }
info() { printf '[repro]       %s\n' "$*"; }

# 1. Cargo.lock tracked in git.
info "check 1: Cargo.lock tracked in git"
CARGO_LOCK="${REPO_ROOT}/agent/Cargo.lock"
if [ ! -f "${CARGO_LOCK}" ]; then
    fail "agent/Cargo.lock does not exist; run 'cargo build' inside agent/"
elif git -C "${REPO_ROOT}" ls-files --error-unmatch agent/Cargo.lock >/dev/null 2>&1; then
    ok "agent/Cargo.lock is tracked by git"
else
    fail "agent/Cargo.lock exists but is not tracked (gitignored or not added)"
fi

# 2. Cargo.lock consistent with Cargo.toml (--locked flag).
info "check 2: Cargo.lock consistent with Cargo.toml"
if command -v cargo >/dev/null 2>&1; then
    if cargo metadata --manifest-path "${REPO_ROOT}/agent/Cargo.toml" \
            --frozen --locked --no-deps --format-version 1 \
            >/dev/null 2>&1; then
        ok "Cargo.lock is consistent with Cargo.toml"
    else
        fail "Cargo.lock is out of sync with Cargo.toml; run 'cargo update' inside agent/"
    fi
else
    info "cargo not found; skipping lock consistency check"
fi

# 3. Go module external dependency check.
info "check 3: Go module external dependencies"
GO_MOD="${REPO_ROOT}/services/go.mod"
GO_SUM="${REPO_ROOT}/services/go.sum"
if [ ! -f "${GO_MOD}" ]; then
    fail "services/go.mod not found"
else
    # Check for external require blocks.
    if ! grep -q '^require' "${GO_MOD}" 2>/dev/null; then
        ok "services/go.mod: no external requires; go.sum not needed"
    elif [ -f "${GO_SUM}" ]; then
        ok "services/go.sum present for external Go dependencies"
    else
        fail "services/go.mod has external requires but services/go.sum is missing; run 'go mod tidy'"
    fi
fi

# 4. VERSIONS.env has required pins.
info "check 4: VERSIONS.env required pins"
VERSIONS_ENV="${SCRIPT_DIR}/VERSIONS.env"
REQUIRED_VARS="NURA_VERSION KERNEL_VERSION MUSL_VERSION BUSYBOX_VERSION RUST_VERSION GO_VERSION"
if [ ! -f "${VERSIONS_ENV}" ]; then
    fail "scripts/VERSIONS.env not found"
else
    missing=""
    for var in ${REQUIRED_VARS}; do
        if ! grep -q "^${var}=" "${VERSIONS_ENV}" 2>/dev/null; then
            missing="${missing} ${var}"
        fi
    done
    if [ -z "${missing}" ]; then
        ok "VERSIONS.env contains all required pins"
    else
        fail "VERSIONS.env missing:${missing}"
    fi
fi

# 5. No secrets committed.
info "check 5: no secrets committed"
if git -C "${REPO_ROOT}" ls-files | grep -qE '(secrets\.toml|\.pem|\.key|\.crt)$'; then
    fail "committed secrets detected; remove them immediately"
else
    ok "no secrets tracked in git"
fi

# Summary.
printf '\n[repro] Results: %d passed, %d failed\n' "${PASS}" "${FAIL}"
if [ "${FAIL}" -gt 0 ]; then
    exit 1
fi
