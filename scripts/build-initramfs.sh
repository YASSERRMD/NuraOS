#!/usr/bin/env bash
# Assemble the NuraOS initramfs from the rootfs staging area and produce
# a cpio.gz archive at image/out/initramfs.cpio.gz.
#
# Prerequisites:
#   scripts/fetch-busybox.sh must have run (rootfs/staging/bin/busybox present)
#
# Usage: ./scripts/build-initramfs.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
STAGING="${REPO_ROOT}/rootfs/_build"
OUT_DIR="${REPO_ROOT}/image/out"
INITRAMFS="${OUT_DIR}/initramfs.cpio.gz"
BB_SRC="${REPO_ROOT}/rootfs/staging/bin/busybox"

log() { printf '[build-initramfs] %s\n' "$*"; }
die() { printf '[build-initramfs] ERROR: %s\n' "$*" >&2; exit 1; }

[ -f "${BB_SRC}" ] || die "busybox binary not found at ${BB_SRC}; run scripts/fetch-busybox.sh first"

for tool in cpio gzip find; do
    command -v "${tool}" >/dev/null 2>&1 || die "required tool not found: ${tool}"
done

log "preparing staging tree at ${STAGING} ..."
rm -rf "${STAGING}"
mkdir -p "${STAGING}"

# ----- Directory layout -----
for d in bin sbin etc proc sys dev data tmp; do
    mkdir -p "${STAGING}/${d}"
done

# ----- Install BusyBox and create applet symlinks -----
log "installing busybox ..."
install -m 755 "${BB_SRC}" "${STAGING}/bin/busybox"

log "creating applet symlinks ..."
# Core applets needed by /init and basic operation.
APPLETS_BIN="sh cat echo ls mkdir ln cp mv rm chmod chown find grep sed \
    sort wc head tail sleep env uname date dmesg kill killall ps"
APPLETS_SBIN="init halt poweroff reboot mount umount switch_root pivot_root \
    ip ping udhcpc mknod"

for applet in ${APPLETS_BIN}; do
    ln -sf /bin/busybox "${STAGING}/bin/${applet}"
done
for applet in ${APPLETS_SBIN}; do
    ln -sf /bin/busybox "${STAGING}/sbin/${applet}"
done

# ----- /init -----
log "installing /init ..."
install -m 755 "${REPO_ROOT}/rootfs/init" "${STAGING}/init"

# ----- /etc -----
log "creating /etc skeleton ..."
echo "nuraos" > "${STAGING}/etc/hostname"
cat > "${STAGING}/etc/fstab" <<'EOF'
proc     /proc  proc     defaults  0 0
sysfs    /sys   sysfs    defaults  0 0
devtmpfs /dev   devtmpfs defaults  0 0
tmpfs    /tmp   tmpfs    defaults  0 0
EOF

# ----- /dev minimal nodes (backup if devtmpfs auto-populate fails) -----
# These are created as regular files in the cpio; the kernel populates /dev
# via devtmpfs at boot, but we add console/null as a failsafe.
# (We cannot use mknod here without root; the cpio header carries device info)

# ----- Build additional staged files (agent, gateway, llama-server) -----
# Future phases install their binaries here.
# Phase 07: only busybox + init.
for extra_dir in sbin; do
    mkdir -p "${STAGING}/${extra_dir}"
done

# ----- Assemble cpio.gz -----
mkdir -p "${OUT_DIR}"
log "assembling initramfs cpio.gz ..."
(
    cd "${STAGING}"
    find . | cpio -H newc -o --quiet 2>/dev/null | gzip -9 > "${INITRAMFS}"
)

SIZE=$(du -h "${INITRAMFS}" | cut -f1)
log "initramfs: ${INITRAMFS} (${SIZE})"
log "done. Boot with: ./scripts/run-qemu.sh"
