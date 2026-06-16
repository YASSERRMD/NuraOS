#!/usr/bin/env bash
# Apply the NuraOS kernel config fragment on top of tinyconfig and run
# olddefconfig to produce a consistent, complete .config in kernel/linux.
#
# Usage: ./scripts/kernel-config.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
LINUX_DIR="${REPO_ROOT}/kernel/linux"
FRAGMENT="${REPO_ROOT}/kernel/configs/nuraos_x86_64_defconfig"

log() { printf '[kernel-config] %s\n' "$*"; }
die() { printf '[kernel-config] ERROR: %s\n' "$*" >&2; exit 1; }

[ -d "${LINUX_DIR}" ] || die "kernel source not found at ${LINUX_DIR}; run scripts/fetch-kernel.sh first"
[ -f "${LINUX_DIR}/Makefile" ] || die "not a kernel tree: ${LINUX_DIR}"
[ -f "${FRAGMENT}" ] || die "config fragment not found: ${FRAGMENT}"

log "starting from tinyconfig ..."
make -C "${LINUX_DIR}" ARCH=x86_64 tinyconfig

log "merging NuraOS config fragment ..."
# kernel/scripts/kconfig/merge_config.sh is available after any prior config step.
"${LINUX_DIR}/scripts/kconfig/merge_config.sh" \
    -m \
    "${LINUX_DIR}/.config" \
    "${FRAGMENT}"

log "running olddefconfig to resolve remaining symbols ..."
make -C "${LINUX_DIR}" ARCH=x86_64 olddefconfig

log ".config written to ${LINUX_DIR}/.config"
log "summary:"
grep -E "^(CONFIG_MODULES|CONFIG_EXT4_FS|CONFIG_VIRTIO|CONFIG_SERIAL_8250|CONFIG_BLK_DEV_INITRD|CONFIG_RANDOMIZE_MEMORY|CONFIG_BUG_ON_DATA_CORRUPTION|CONFIG_EARLY_PRINTK|CONFIG_SERIAL_EARLYCON)=" \
    "${LINUX_DIR}/.config" | sed 's/^/  /' || true

log "done."
