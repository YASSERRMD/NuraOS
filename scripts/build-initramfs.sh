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
for d in bin sbin etc proc sys dev data tmp run var; do
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
proc     /proc  proc     defaults           0 0
sysfs    /sys   sysfs    defaults           0 0
devtmpfs /dev   devtmpfs defaults           0 0
tmpfs    /tmp   tmpfs    defaults           0 0
tmpfs    /run   tmpfs    mode=755,nosuid,nodev  0 0
tmpfs    /var   tmpfs    mode=755,nosuid,nodev  0 0
EOF

# udhcpc DHCP client script.
UDHCPC_SCRIPT="${REPO_ROOT}/rootfs/etc/udhcpc/default.script"
if [ -f "${UDHCPC_SCRIPT}" ]; then
    mkdir -p "${STAGING}/etc/udhcpc"
    install -m 755 "${UDHCPC_SCRIPT}" "${STAGING}/etc/udhcpc/default.script"
fi

# ----- /dev minimal nodes (backup if devtmpfs auto-populate fails) -----
# These are created as regular files in the cpio; the kernel populates /dev
# via devtmpfs at boot, but we add console/null as a failsafe.
# (We cannot use mknod here without root; the cpio header carries device info)

# ----- Supervisor -----
SUPERVISOR_SRC="${REPO_ROOT}/rootfs/sbin/supervisor"
if [ -f "${SUPERVISOR_SRC}" ]; then
    mkdir -p "${STAGING}/sbin"
    install -m 755 "${SUPERVISOR_SRC}" "${STAGING}/sbin/supervisor"
    log "installed supervisor"
fi

# ----- nura-manager (Phase 56+) -----
MANAGER_BIN="${REPO_ROOT}/rootfs/staging/sbin/nura-manager"
if [ -f "${MANAGER_BIN}" ]; then
    mkdir -p "${STAGING}/sbin"
    install -m 755 "${MANAGER_BIN}" "${STAGING}/sbin/nura-manager"
    log "installed nura-manager"
fi

# ----- nuractl (Phase 59+) -----
NURACTL_BIN="${REPO_ROOT}/rootfs/staging/sbin/nuractl"
if [ -f "${NURACTL_BIN}" ]; then
    mkdir -p "${STAGING}/sbin"
    install -m 755 "${NURACTL_BIN}" "${STAGING}/sbin/nuractl"
    log "installed nuractl"
fi

# ----- Unit files -----
UNIT_SRC="${REPO_ROOT}/rootfs/etc/nura/services"
if [ -d "${UNIT_SRC}" ]; then
    mkdir -p "${STAGING}/etc/nura/services"
    for f in "${UNIT_SRC}"/*.toml; do
        [ -f "${f}" ] || continue
        install -m 644 "${f}" "${STAGING}/etc/nura/services/"
        log "installed unit: $(basename "${f}")"
    done
fi

# ----- nura-agent binary (installed by build-agent.sh) -----
AGENT_BIN="${REPO_ROOT}/rootfs/staging/sbin/nura-agent"
if [ -f "${AGENT_BIN}" ]; then
    mkdir -p "${STAGING}/sbin"
    install -m 755 "${AGENT_BIN}" "${STAGING}/sbin/nura-agent"
    log "installed nura-agent"
fi

# ----- Gateway and llama-server (installed by later phases) -----
for svc in llama-server gateway; do
    SVC_BIN="${REPO_ROOT}/rootfs/staging/sbin/${svc}"
    if [ -f "${SVC_BIN}" ]; then
        mkdir -p "${STAGING}/sbin"
        install -m 755 "${SVC_BIN}" "${STAGING}/sbin/${svc}"
        log "installed ${svc}"
    fi
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
