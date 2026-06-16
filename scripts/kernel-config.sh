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

# Save a copy of the resolved .config into the image output dir so CI uploads it
# as part of the nuraos-image artifact for offline inspection.
OUT_DIR="${REPO_ROOT}/image/out"
mkdir -p "${OUT_DIR}"
cp "${LINUX_DIR}/.config" "${OUT_DIR}/kernel.config" || true

# Verify that critical symbols actually SURVIVED olddefconfig.  olddefconfig
# silently drops any symbol whose dependencies are unmet, so a symbol present in
# the fragment is NOT guaranteed to be in the final .config.  Print the full
# state (including "is not set") of the symbols that matter for boot + serial.
log "post-olddefconfig state of critical symbols:"
for sym in \
    CONFIG_TTY CONFIG_SERIAL_CORE CONFIG_SERIAL_CORE_CONSOLE \
    CONFIG_SERIAL_8250 CONFIG_SERIAL_8250_CONSOLE CONFIG_SERIAL_8250_PCI \
    CONFIG_SERIAL_8250_NR_UARTS CONFIG_SERIAL_8250_RUNTIME_UARTS \
    CONFIG_SERIAL_EARLYCON CONFIG_EARLY_PRINTK CONFIG_HAS_IOPORT \
    CONFIG_KERNEL_GZIP CONFIG_KERNEL_XZ \
    CONFIG_VIRTIO CONFIG_VIRTIO_PCI CONFIG_VIRTIO_BLK CONFIG_VIRTIO_NET \
    CONFIG_EXT4_FS CONFIG_BLK_DEV_INITRD CONFIG_DEVTMPFS; do
    line=$(grep -E "^(${sym}=| *# ${sym} is not set)" "${LINUX_DIR}/.config" || true)
    if [ -z "${line}" ]; then
        line="${sym} ABSENT (not in .config at all)"
    fi
    printf '  %s\n' "${line}"
done

# Warn loudly if the serial console driver was dropped: without it the guest
# produces zero serial output and every integration suite fails to boot.
# (Diagnostic build -- once the dependency is fixed this becomes a hard die.)
if ! grep -qE "^CONFIG_SERIAL_8250=y" "${LINUX_DIR}/.config"; then
    log "WARNING: CONFIG_SERIAL_8250 was DROPPED by olddefconfig -- guest will have NO serial console!"
    log "WARNING: inspect the symbol state above to find the unmet dependency."
fi

log "done."
