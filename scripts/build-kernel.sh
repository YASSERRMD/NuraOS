#!/usr/bin/env bash
# Build the Linux kernel using the NuraOS config and emit bzImage to image/out.
#
# Prerequisites:
#   1. ./scripts/fetch-kernel.sh  (kernel source at kernel/linux)
#   2. ./scripts/kernel-config.sh (kernel/linux/.config present)
#
# Usage: ./scripts/build-kernel.sh [-j N]
#   -j N  parallel job count (default: number of CPU cores)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
LINUX_DIR="${REPO_ROOT}/kernel/linux"
OUT_DIR="${REPO_ROOT}/image/out"

log() { printf '[build-kernel] %s\n' "$*"; }
die() { printf '[build-kernel] ERROR: %s\n' "$*" >&2; exit 1; }

# Parse -j flag.
JOBS=$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)
while [[ $# -gt 0 ]]; do
    case "$1" in
        -j) shift; JOBS="$1" ;;
        -j*) JOBS="${1#-j}" ;;
        *) die "unknown argument: $1" ;;
    esac
    shift
done

[ -d "${LINUX_DIR}" ] || die "kernel source not found; run scripts/fetch-kernel.sh"
[ -f "${LINUX_DIR}/.config" ] || die ".config missing; run scripts/kernel-config.sh first"

mkdir -p "${OUT_DIR}"

log "building bzImage with ${JOBS} jobs ..."
START=$(date +%s)

make -C "${LINUX_DIR}" ARCH=x86_64 bzImage -j"${JOBS}"

END=$(date +%s)
ELAPSED=$((END - START))

BZIMAGE="${LINUX_DIR}/arch/x86/boot/bzImage"
[ -f "${BZIMAGE}" ] || die "bzImage not produced after build"

cp "${BZIMAGE}" "${OUT_DIR}/bzImage"
SIZE=$(du -h "${OUT_DIR}/bzImage" | cut -f1)

log "bzImage: ${OUT_DIR}/bzImage"
log "size:    ${SIZE}"
log "build time: ${ELAPSED}s"

# Append size record to docs/kernel.md.
KERNEL_DOC="${REPO_ROOT}/docs/kernel.md"
if [ -f "${KERNEL_DOC}" ]; then
    grep -q "(TBD)" "${KERNEL_DOC}" && \
        sed -i.bak "s|(TBD).*first build attempt|${SIZE}  | first build (Phase 04)|" \
        "${KERNEL_DOC}" && rm -f "${KERNEL_DOC}.bak" || true
fi

log "done. Boot it with: ./scripts/run-qemu.sh"
