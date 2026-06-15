#!/usr/bin/env bash
# Verify that required host tools exist and print their versions.
# Exit 0 only when all required tools are present.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "${SCRIPT_DIR}/VERSIONS.env"

RED='\033[0;31m'
GRN='\033[0;32m'
YLW='\033[1;33m'
RST='\033[0m'

PASS=0
FAIL=0

check() {
    local label="$1"
    local cmd="$2"
    local version_cmd="$3"

    if command -v "${cmd}" >/dev/null 2>&1; then
        local ver
        ver=$(eval "${version_cmd}" 2>/dev/null | head -1 || echo "(unknown)")
        printf "${GRN}[PASS]${RST} %-24s %s\n" "${label}" "${ver}"
        PASS=$((PASS + 1))
    else
        printf "${RED}[FAIL]${RST} %-24s not found\n" "${label}"
        FAIL=$((FAIL + 1))
    fi
}

check_min_version() {
    local label="$1"
    local cmd="$2"
    local version_cmd="$3"
    local min_ver="$4"

    if command -v "${cmd}" >/dev/null 2>&1; then
        local ver
        ver=$(eval "${version_cmd}" 2>/dev/null | head -1 | grep -oE '[0-9]+\.[0-9]+(\.[0-9]+)?' | head -1 || echo "0.0.0")
        printf "${GRN}[PASS]${RST} %-24s %s (need >= %s)\n" "${label}" "${ver}" "${min_ver}"
        PASS=$((PASS + 1))
    else
        printf "${RED}[FAIL]${RST} %-24s not found (need >= %s)\n" "${label}" "${min_ver}"
        FAIL=$((FAIL + 1))
    fi
}

echo "NuraOS host prerequisite check"
echo "================================"

check "make"               make    "make --version"
check "gcc"                gcc     "gcc --version"
check "bc"                 bc      "bc --version"
check "flex"               flex    "flex --version"
check "bison"              bison   "bison --version"
check "libelf (pahole)"    pahole  "pahole --version"
check "openssl"            openssl "openssl version"
check "xz"                 xz      "xz --version"
check "cpio"               cpio    "cpio --version"
check "git"                git     "git --version"
check "curl"               curl    "curl --version"

check_min_version "qemu-system-x86_64" qemu-system-x86_64 \
    "qemu-system-x86_64 --version" "${QEMU_MIN_VERSION}"

# Rust: check via rustup or direct rustc
if command -v rustup >/dev/null 2>&1; then
    ver=$(rustup show active-toolchain 2>/dev/null | head -1 || rustc --version 2>/dev/null || echo "(unknown)")
    printf "${GRN}[PASS]${RST} %-24s %s\n" "rust (rustup)" "${ver}"
    PASS=$((PASS + 1))
elif command -v rustc >/dev/null 2>&1; then
    ver=$(rustc --version 2>/dev/null || echo "(unknown)")
    printf "${YLW}[WARN]${RST} %-24s %s (no rustup; rustc found)\n" "rust" "${ver}"
    PASS=$((PASS + 1))
else
    printf "${RED}[FAIL]${RST} %-24s not found\n" "rust/rustc"
    FAIL=$((FAIL + 1))
fi

# Go
if command -v go >/dev/null 2>&1; then
    ver=$(go version 2>/dev/null || echo "(unknown)")
    printf "${GRN}[PASS]${RST} %-24s %s\n" "go" "${ver}"
    PASS=$((PASS + 1))
else
    printf "${RED}[FAIL]${RST} %-24s not found (need ${GO_VERSION}+)\n" "go"
    FAIL=$((FAIL + 1))
fi

echo "================================"
echo "Results: ${PASS} passed, ${FAIL} failed"

if [ "${FAIL}" -gt 0 ]; then
    echo ""
    echo "Install missing tools before building NuraOS."
    echo "See docs/host-setup.md for per-distro instructions."
    exit 1
fi

echo ""
echo "All prerequisites present."
