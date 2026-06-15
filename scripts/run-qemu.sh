#!/usr/bin/env bash
# Boot NuraOS in QEMU with serial console on stdio.
#
# Usage: ./scripts/run-qemu.sh [OPTIONS]
#
# Options:
#   --initramfs PATH   path to initramfs.cpio.gz (default: image/out/initramfs.cpio.gz)
#   --data PATH        path to /data ext4 image  (default: image/out/data.img)
#   --kernel PATH      path to bzImage           (default: image/out/bzImage)
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
LOG_FILE="${OUT_DIR}/boot.log"
USE_INITRAMFS=1
USE_DATA=1
MEM=512
CPUS=2
PORT_API=8080
PORT_METRICS=9090
TIMEOUT=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --initramfs) shift; INITRAMFS="$1" ;;
        --data)      shift; DATA_IMG="$1" ;;
        --kernel)    shift; KERNEL="$1" ;;
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

[ -f "${KERNEL}" ] || die "bzImage not found at ${KERNEL}; run scripts/build-kernel.sh"

mkdir -p "$(dirname "${LOG_FILE}")"

# Build the QEMU command line.
QEMU_ARGS=(
    -machine q35,accel=tcg
    -cpu qemu64
    -m "${MEM}M"
    -smp "${CPUS}"
    -nographic
    -serial "mon:stdio"
    -no-reboot
    -kernel "${KERNEL}"
)

# Kernel command line.
KCMDLINE="console=ttyS0,115200 panic=5 loglevel=7"

if [ "${USE_INITRAMFS}" -eq 1 ] && [ -f "${INITRAMFS}" ]; then
    QEMU_ARGS+=(-initrd "${INITRAMFS}")
    log "initramfs: ${INITRAMFS}"
elif [ "${USE_INITRAMFS}" -eq 1 ]; then
    log "WARNING: initramfs not found at ${INITRAMFS}; booting without it (expect kernel panic)"
fi

if [ "${USE_DATA}" -eq 1 ] && [ -f "${DATA_IMG}" ]; then
    QEMU_ARGS+=(
        -drive "file=${DATA_IMG},format=raw,if=virtio,cache=writeback"
    )
    log "data disk: ${DATA_IMG}"
fi

# User-mode networking with host port forwards.
QEMU_ARGS+=(
    -netdev "user,id=net0,hostfwd=tcp::${PORT_API}-:8080,hostfwd=tcp::${PORT_METRICS}-:9090"
    -device "virtio-net-pci,netdev=net0"
)

QEMU_ARGS+=(-append "${KCMDLINE}")

log "kernel: ${KERNEL}"
log "memory: ${MEM}M  cpus: ${CPUS}"
log "API port: ${PORT_API}  metrics port: ${PORT_METRICS}"
log "serial log: ${LOG_FILE}"
[ "${TIMEOUT}" -gt 0 ] && log "timeout: ${TIMEOUT}s"
log "---"

START=$(date +%s)

# Run QEMU, tee serial output to log.
if [ "${TIMEOUT}" -gt 0 ]; then
    timeout "${TIMEOUT}" qemu-system-x86_64 "${QEMU_ARGS[@]}" 2>&1 | tee "${LOG_FILE}" || true
else
    qemu-system-x86_64 "${QEMU_ARGS[@]}" 2>&1 | tee "${LOG_FILE}" || true
fi

END=$(date +%s)
log "QEMU exited after $((END - START))s"
