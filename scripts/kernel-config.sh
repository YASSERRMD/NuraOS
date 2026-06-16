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
# merge_config.sh defaults its output dir to $PWD (OUTPUT=.), so without -O it
# writes the merged .config to the repo root -- NOT into the kernel tree -- and
# the subsequent `make -C "${LINUX_DIR}" olddefconfig` then reads the UN-merged
# tinyconfig at "${LINUX_DIR}/.config", silently discarding the entire fragment
# (no TTY/serial/printk/virtio/block -> guest boots with zero console output).
# -O "${LINUX_DIR}" forces the merged result into the kernel tree where the
# build and olddefconfig actually read it.
"${LINUX_DIR}/scripts/kconfig/merge_config.sh" \
    -m \
    -O "${LINUX_DIR}" \
    "${LINUX_DIR}/.config" \
    "${FRAGMENT}"

# Sanity-check that the merge actually landed in the kernel tree's .config
# (guards against the OUTPUT-dir bug above regressing).  A sentinel symbol that
# is OFF in tinyconfig but ON in the fragment must now be present.
if ! grep -qE "^CONFIG_SERIAL_8250=y" "${LINUX_DIR}/.config"; then
    die "fragment merge did not land in ${LINUX_DIR}/.config (CONFIG_SERIAL_8250 missing post-merge) -- check merge_config.sh -O output dir"
fi

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
    CONFIG_TTY CONFIG_SERIAL_8250 CONFIG_SERIAL_8250_CONSOLE \
    CONFIG_SERIAL_EARLYCON CONFIG_EARLY_PRINTK CONFIG_PRINTK \
    CONFIG_FUTEX CONFIG_MEMBARRIER CONFIG_ADVISE_SYSCALLS \
    CONFIG_BUG CONFIG_BASE_FULL CONFIG_FHANDLE CONFIG_POSIX_MQUEUE \
    CONFIG_VIRTIO_MENU CONFIG_VIRTIO CONFIG_VIRTIO_PCI \
    CONFIG_VIRTIO_BLK CONFIG_VIRTIO_NET CONFIG_HW_RANDOM_VIRTIO \
    CONFIG_EXT4_FS CONFIG_BLK_DEV_INITRD CONFIG_DEVTMPFS; do
    line=$(grep -E "^(${sym}=| *# ${sym} is not set)" "${LINUX_DIR}/.config" || true)
    if [ -z "${line}" ]; then
        line="${sym} ABSENT (not in .config at all)"
    fi
    printf '  %s\n' "${line}"
done

# Hard-fail the build if the serial console driver did not survive olddefconfig:
# without it the guest produces zero serial output and every integration suite
# fails to boot. This catches both the merge-output-dir bug and any future
# dependency regression.
if ! grep -qE "^CONFIG_SERIAL_8250=y" "${LINUX_DIR}/.config"; then
    die "CONFIG_SERIAL_8250 not set in final .config -- guest would have NO serial console. Inspect the symbol state dumped above."
fi
if ! grep -qE "^CONFIG_TTY=y" "${LINUX_DIR}/.config"; then
    die "CONFIG_TTY not set in final .config -- the entire serial subsystem is gated behind it."
fi
if ! grep -qE "^CONFIG_FUTEX=y" "${LINUX_DIR}/.config"; then
    die "CONFIG_FUTEX not set in final .config -- the futex syscall returns ENOSYS and every Go binary (nura-manager, gateway) segfaults immediately."
fi
if ! grep -qE "^CONFIG_VIRTIO_MENU=y" "${LINUX_DIR}/.config"; then
    die "CONFIG_VIRTIO_MENU not set in final .config -- all virtio drivers (net/blk/rng) are gated behind it and will be dropped."
fi
if ! grep -qE "^CONFIG_VIRTIO_NET=y" "${LINUX_DIR}/.config"; then
    die "CONFIG_VIRTIO_NET not set in final .config (needs CONFIG_NETDEVICES=y) -- no eth0, DHCP fails, and the harness /healthz probe is unreachable via hostfwd."
fi

log "done."
