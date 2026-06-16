#!/usr/bin/env bash
# Boot NuraOS in QEMU with serial console on stdio.
#
# Two boot modes:
#   Direct kernel (default): -kernel/-initrd flags; no real bootloader.
#   Bootloader disk (--disk): boots from a disk image built by build-boot.sh;
#     exercises the extlinux boot path with A/B slot and recovery menu.
#
# Typical usage after build-image.sh:
#   ./scripts/run-qemu.sh
#
# Bootloader path (after build-boot.sh):
#   ./scripts/run-qemu.sh --disk image/out/disk.img
#
# Usage: ./scripts/run-qemu.sh [OPTIONS]
#
# Options:
#   --disk PATH        boot from disk image (real bootloader path)
#   --initramfs PATH   path to initramfs.cpio.gz (default: image/out/initramfs.cpio.gz)
#   --data PATH        path to /data ext4 image  (default: image/out/data.img)
#   --kernel PATH      path to bzImage           (default: image/out/bzImage)
#   --manifest PATH    read artifact paths from manifest.json (default: image/out/manifest.json)
#   --log PATH         capture serial log to this file (default: image/out/boot.log)
#   --no-initramfs     boot without initramfs (kernel panic expected -- Phase 04)
#   --no-data          boot without /data disk
#   --mem MB           RAM for the VM in megabytes (default: 512)
#   --cpus N           vCPU count (default: 2)
#   --port-api N       host port forwarded to guest HTTP API (default: 8080)
#   --port-metrics N   host port forwarded to guest metrics (default: 9090)
#   --timeout N        kill QEMU after N seconds (0 = no timeout, default: 0)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUT_DIR="${REPO_ROOT}/image/out"

KERNEL="${OUT_DIR}/bzImage"
INITRAMFS="${OUT_DIR}/initramfs.cpio.gz"
DATA_IMG="${OUT_DIR}/data.img"
MANIFEST="${OUT_DIR}/manifest.json"
LOG_FILE="${OUT_DIR}/boot.log"
DISK_IMG=""
USE_INITRAMFS=1
USE_DATA=1
MEM=512
CPUS=2
PORT_API=8080
PORT_METRICS=9090
TIMEOUT=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --disk)      shift; DISK_IMG="$1" ;;
        --initramfs) shift; INITRAMFS="$1" ;;
        --data)      shift; DATA_IMG="$1" ;;
        --kernel)    shift; KERNEL="$1" ;;
        --manifest)  shift; MANIFEST="$1" ;;
        --log)       shift; LOG_FILE="$1" ;;
        --no-initramfs) USE_INITRAMFS=0 ;;
        --no-data)      USE_DATA=0 ;;
        --mem)       shift; MEM="$1" ;;
        --cpus)      shift; CPUS="$1" ;;
        --port-api)  shift; PORT_API="$1" ;;
        --port-metrics) shift; PORT_METRICS="$1" ;;
        --timeout)   shift; TIMEOUT="$1" ;;
        *) echo "unknown argument: $1" >&2; exit 1 ;;
    esac
    shift
done

log() { printf '[run-qemu] %s\n' "$*"; }
die() { printf '[run-qemu] ERROR: %s\n' "$*" >&2; exit 1; }

command -v qemu-system-x86_64 >/dev/null 2>&1 || \
    die "qemu-system-x86_64 not found; install QEMU >= 8.2.0"

# If a manifest exists, print version info and honour its artifact paths when
# the caller did not supply explicit overrides.
if [ -f "${MANIFEST}" ]; then
    nura_ver=$(grep '"nura_version"' "${MANIFEST}" | sed 's/.*: *"\([^"]*\)".*/\1/')
    kernel_ver=$(grep '"kernel_version"' "${MANIFEST}" | sed 's/.*: *"\([^"]*\)".*/\1/')
    log "manifest: nura=${nura_ver}  kernel=${kernel_ver}"
fi

mkdir -p "$(dirname "${LOG_FILE}")"

# Base QEMU args shared by both boot modes.
# shellcheck disable=SC2054  # commas are intentional QEMU syntax, not array separators
QEMU_ARGS=(
    -machine q35,accel=tcg
    -cpu qemu64
    -m "${MEM}M"
    -smp "${CPUS}"
    -nographic
    -serial "mon:stdio"
    -no-reboot
)

# User-mode networking with host port forwards.
QEMU_ARGS+=(
    -netdev "user,id=net0,hostfwd=tcp::${PORT_API}-:8080,hostfwd=tcp::${PORT_METRICS}-:9090"
    -device "virtio-net-pci,netdev=net0"
)

# /data drive (shared by both modes).
if [ "${USE_DATA}" -eq 1 ] && [ -f "${DATA_IMG}" ]; then
    QEMU_ARGS+=(
        -drive "file=${DATA_IMG},format=raw,if=virtio,cache=writeback"
    )
    log "data disk: ${DATA_IMG}"
fi

if [ -n "${DISK_IMG}" ]; then
    # ----------------------------------------------------------------
    # Bootloader disk mode: boot from a disk image built by build-boot.sh.
    # The disk image contains the MBR, syslinux bootloader, kernel,
    # initramfs, and extlinux.conf with the A/B + recovery menu.
    # ----------------------------------------------------------------
    [ -f "${DISK_IMG}" ] || die "disk image not found at ${DISK_IMG}; run scripts/build-boot.sh"
    log "BOOT MODE: bootloader disk (extlinux)"
    log "disk image: ${DISK_IMG}"
    QEMU_ARGS+=(
        -drive "file=${DISK_IMG},format=raw,if=ide,index=0,media=disk"
        -boot "order=c,menu=on"
    )
else
    # ----------------------------------------------------------------
    # Direct kernel mode (default): QEMU -kernel shortcut.
    # Faster iteration; no real bootloader is exercised.
    # ----------------------------------------------------------------
    [ -f "${KERNEL}" ] || die "bzImage not found at ${KERNEL}; run scripts/build-image.sh"
    log "BOOT MODE: direct kernel (-kernel)"
    log "kernel: ${KERNEL}"

    KCMDLINE="console=ttyS0,115200 panic=5 loglevel=7"
    QEMU_ARGS+=(-kernel "${KERNEL}")

    if [ "${USE_INITRAMFS}" -eq 1 ] && [ -f "${INITRAMFS}" ]; then
        QEMU_ARGS+=(-initrd "${INITRAMFS}")
        log "initramfs: ${INITRAMFS}"
    elif [ "${USE_INITRAMFS}" -eq 1 ]; then
        log "WARNING: initramfs not found at ${INITRAMFS}; booting without it (expect kernel panic)"
    fi

    QEMU_ARGS+=(-append "${KCMDLINE}")
fi

log "memory: ${MEM}M  cpus: ${CPUS}"
log "API port: ${PORT_API}  metrics port: ${PORT_METRICS}"
log "serial log: ${LOG_FILE}"
[ "${TIMEOUT}" -gt 0 ] && log "timeout: ${TIMEOUT}s"
log "---"

START=$(date +%s)

# Pick a portable timeout command: GNU coreutils ships `timeout` on Linux and
# `gtimeout` on macOS (via `brew install coreutils`). macOS has neither by
# default, so fall back to running without a hard timeout rather than crashing.
TIMEOUT_CMD=""
if command -v timeout >/dev/null 2>&1; then
    TIMEOUT_CMD="timeout"
elif command -v gtimeout >/dev/null 2>&1; then
    TIMEOUT_CMD="gtimeout"
fi

# Run QEMU, tee serial output to log.
if [ "${TIMEOUT}" -gt 0 ] && [ -n "${TIMEOUT_CMD}" ]; then
    "${TIMEOUT_CMD}" "${TIMEOUT}" qemu-system-x86_64 "${QEMU_ARGS[@]}" 2>&1 | tee "${LOG_FILE}" || true
else
    if [ "${TIMEOUT}" -gt 0 ]; then
        log "WARNING: no 'timeout'/'gtimeout' found; running without a hard timeout"
        log "         (stop with Ctrl-A x, or 'brew install coreutils' for --timeout on macOS)"
    fi
    qemu-system-x86_64 "${QEMU_ARGS[@]}" 2>&1 | tee "${LOG_FILE}" || true
fi

END=$(date +%s)
log "QEMU exited after $((END - START))s"
