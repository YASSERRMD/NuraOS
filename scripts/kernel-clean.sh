#!/usr/bin/env bash
# Wrapper around `make mrproper` for the kernel tree.
# Removes all generated files, config, and build artifacts from kernel/linux.
#
# Usage: ./scripts/kernel-clean.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
LINUX_DIR="${REPO_ROOT}/kernel/linux"

log() { printf '[kernel-clean] %s\n' "$*"; }
die() { printf '[kernel-clean] ERROR: %s\n' "$*" >&2; exit 1; }

[ -d "${LINUX_DIR}" ] || die "kernel source not found at ${LINUX_DIR}; run scripts/fetch-kernel.sh first"
[ -f "${LINUX_DIR}/Makefile" ] || die "not a kernel tree: ${LINUX_DIR}"

log "running make mrproper in ${LINUX_DIR} ..."
make -C "${LINUX_DIR}" mrproper

log "kernel tree cleaned."
