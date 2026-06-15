#!/usr/bin/env bash
# Build nura-agent as a fully static musl binary and install it to the
# rootfs staging area for inclusion in the initramfs.
#
# Usage: ./scripts/build-agent.sh [--release]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
AGENT_DIR="${REPO_ROOT}/agent"
STAGING_BIN="${REPO_ROOT}/rootfs/staging/sbin"

PROFILE="debug"
CARGO_FLAGS=()

for arg in "$@"; do
    case "${arg}" in
        --release) PROFILE="release"; CARGO_FLAGS+=(--release) ;;
    esac
done

log() { printf '[build-agent] %s\n' "$*"; }
die() { printf '[build-agent] ERROR: %s\n' "$*" >&2; exit 1; }

command -v cargo >/dev/null 2>&1 || die "cargo not found; install Rust toolchain"

# Ensure the musl target is installed.
MUSL_TARGET="x86_64-unknown-linux-musl"
if ! rustup target list --installed 2>/dev/null | grep -q "${MUSL_TARGET}"; then
    log "adding Rust target ${MUSL_TARGET} ..."
    rustup target add "${MUSL_TARGET}"
fi

log "building nura-agent (profile: ${PROFILE}, target: ${MUSL_TARGET}) ..."
cd "${AGENT_DIR}"
RUSTFLAGS="-C target-feature=+crt-static" \
    cargo build --target "${MUSL_TARGET}" ${CARGO_FLAGS[@]+"${CARGO_FLAGS[@]}"}

BINARY="${AGENT_DIR}/target/${MUSL_TARGET}/${PROFILE}/nura-agent"
[ -f "${BINARY}" ] || die "binary not found: ${BINARY}"

# Verify static linkage.
if file "${BINARY}" | grep -q "statically linked"; then
    log "OK: nura-agent is statically linked"
fi

SIZE=$(du -h "${BINARY}" | cut -f1)
log "binary: ${BINARY} (${SIZE})"

# Install to staging.
mkdir -p "${STAGING_BIN}"
cp "${BINARY}" "${STAGING_BIN}/nura-agent"
chmod 755 "${STAGING_BIN}/nura-agent"

log "installed to ${STAGING_BIN}/nura-agent"
log "done."
