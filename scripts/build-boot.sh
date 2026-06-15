#!/usr/bin/env bash
# Build a QEMU-bootable NuraOS disk image with an extlinux boot partition.
#
# Disk image layout (GPT/MBR):
#   Sector 0       : MBR (syslinux MBR or zeroes)
#   Sectors 2048+  : Partition 1 (FAT16, 64 MiB) -- the boot partition:
#                      /bzImage
#                      /initramfs.cpio.gz
#                      /extlinux.conf
#                      /cmdline.conf
#                      /ldlinux.sys (extlinux loader)
#                      /menu.c32    (menu module, if available)
#
# Output: image/out/disk.img
#
# Required tools (checked at startup):
#   dd, sfdisk, mkfs.vfat (util-linux/dosfstools), extlinux (syslinux), mtools
#
# Usage:
#   ./scripts/build-boot.sh [OPTIONS]
#
# Options:
#   --kernel PATH      bzImage path       (default: image/out/bzImage)
#   --initramfs PATH   initramfs.cpio.gz  (default: image/out/initramfs.cpio.gz)
#   --slot a|b         active slot        (default: read from data-root/etc/active-slot or a)
#   --data-dir PATH    /data root         (default: image/out/data-root)
#   --boot-dir PATH    boot staging dir   (default: image/out/boot)
#   --out PATH         disk image output  (default: image/out/disk.img)
#   --size-mb N        disk image size    (default: 128)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUT_DIR="${REPO_ROOT}/image/out"

KERNEL="${OUT_DIR}/bzImage"
INITRAMFS="${OUT_DIR}/initramfs.cpio.gz"
BOOT_DIR="${OUT_DIR}/boot"
DATA_DIR="${OUT_DIR}/data-root"
DISK_IMG="${OUT_DIR}/disk.img"
SLOT=""
DISK_MB=128

while [[ $# -gt 0 ]]; do
    case "$1" in
        --kernel)     shift; KERNEL="$1" ;;
        --initramfs)  shift; INITRAMFS="$1" ;;
        --boot-dir)   shift; BOOT_DIR="$1" ;;
        --data-dir)   shift; DATA_DIR="$1" ;;
        --out)        shift; DISK_IMG="$1" ;;
        --slot)       shift; SLOT="$1" ;;
        --size-mb)    shift; DISK_MB="$1" ;;
        *) printf '[build-boot] unknown argument: %s\n' "$1" >&2; exit 1 ;;
    esac
    shift
done

log() { printf '[build-boot] %s\n' "$*"; }
die() { printf '[build-boot] ERROR: %s\n' "$*" >&2; exit 1; }

# --- Resolve active slot ---
ACTIVE_SLOT_FILE="${DATA_DIR}/etc/active-slot"
if [ -z "${SLOT}" ]; then
    if [ -f "${ACTIVE_SLOT_FILE}" ]; then
        SLOT=$(tr -d '[:space:]' < "${ACTIVE_SLOT_FILE}")
    else
        SLOT="a"
    fi
fi

# --- Check required tools ---
MISSING_TOOLS=()
for tool in dd sfdisk mkfs.vfat extlinux; do
    command -v "${tool}" >/dev/null 2>&1 || MISSING_TOOLS+=("${tool}")
done

# mtools optional (improves file copy without root).
HAS_MTOOLS=1
for mt in mformat mcopy; do
    command -v "${mt}" >/dev/null 2>&1 || { HAS_MTOOLS=0; break; }
done

if [ "${#MISSING_TOOLS[@]}" -gt 0 ]; then
    log "missing required tools: ${MISSING_TOOLS[*]}"
    log ""
    log "Install hints (Debian/Ubuntu):"
    log "  sudo apt-get install syslinux syslinux-common dosfstools mtools"
    log ""
    log "Install hints (macOS with Homebrew):"
    log "  brew install syslinux dosfstools mtools"
    log ""
    log "Skipping disk image build. Boot config files are still generated in:"
    log "  ${BOOT_DIR}/"
    # Generate config files anyway so the developer can inspect them.
    "${SCRIPT_DIR}/boot-config.sh" --boot-dir "${BOOT_DIR}" --data-dir "${DATA_DIR}" \
        ${SLOT:+--slot "${SLOT}"}
    exit 0
fi

[ -f "${KERNEL}" ]    || die "bzImage not found at ${KERNEL}; run scripts/build-image.sh"
[ -f "${INITRAMFS}" ] || die "initramfs not found at ${INITRAMFS}; run scripts/build-initramfs.sh"

# --- Generate boot config ---
"${SCRIPT_DIR}/boot-config.sh" --boot-dir "${BOOT_DIR}" --data-dir "${DATA_DIR}" \
    ${SLOT:+--slot "${SLOT}"}

# --- Disk geometry ---
SECTOR_SZ=512
DISK_SECTORS=$(( DISK_MB * 1024 * 1024 / SECTOR_SZ ))
PART_START=2048                               # 1 MiB aligned
PART_SECTORS=$(( DISK_SECTORS - PART_START ))
PART_SIZE_BYTES=$(( PART_SECTORS * SECTOR_SZ ))

log "creating disk image: ${DISK_IMG} (${DISK_MB} MiB)"
dd if=/dev/zero of="${DISK_IMG}" bs=1M count="${DISK_MB}" status=progress 2>/dev/null || \
    dd if=/dev/zero of="${DISK_IMG}" bs=1M count="${DISK_MB}"

# --- Partition table (MBR / DOS) ---
log "writing partition table"
sfdisk "${DISK_IMG}" << PTABLE
label: dos
unit: sectors

/dev/loop0p1 : start=${PART_START}, size=${PART_SECTORS}, type=b, bootable
PTABLE

# --- Create FAT16 filesystem in the partition ---
PART_OFFSET=$(( PART_START * SECTOR_SZ ))
log "formatting boot partition (FAT16, offset=${PART_OFFSET})"

if [ "${HAS_MTOOLS}" -eq 1 ]; then
    # mtools path: no root needed.
    MTOOLS_CFG="${BOOT_DIR}/.mtoolsrc"
    cat > "${MTOOLS_CFG}" << MTRC
drive b:
    file="${DISK_IMG}"
    offset=${PART_OFFSET}
    partition=0
MTRC
    export MTOOLSRC="${MTOOLS_CFG}"
    mformat -i "${DISK_IMG}@@${PART_OFFSET}" -F -v NURAB ::
    log "copying kernel, initramfs, and boot config to partition"
    mcopy -i "${DISK_IMG}@@${PART_OFFSET}" "${KERNEL}"    ::/bzImage
    mcopy -i "${DISK_IMG}@@${PART_OFFSET}" "${INITRAMFS}" ::/initramfs.cpio.gz
    mcopy -i "${DISK_IMG}@@${PART_OFFSET}" "${BOOT_DIR}/extlinux.conf" ::/extlinux.conf
    mcopy -i "${DISK_IMG}@@${PART_OFFSET}" "${BOOT_DIR}/cmdline.conf"  ::/cmdline.conf
    rm -f "${MTOOLS_CFG}"
else
    # loopback path: requires root or loop device access.
    log "mtools not found; using loop device (may require sudo)"
    LOOP_DEV=$(losetup --find --show --offset "${PART_OFFSET}" \
        --sizelimit "${PART_SIZE_BYTES}" "${DISK_IMG}")
    trap 'losetup -d "${LOOP_DEV}" 2>/dev/null || true' EXIT
    mkfs.vfat -F 16 -n NURAB "${LOOP_DEV}"
    MNT_DIR=$(mktemp -d)
    mount -t vfat "${LOOP_DEV}" "${MNT_DIR}"
    trap 'umount "${MNT_DIR}" 2>/dev/null || true; losetup -d "${LOOP_DEV}" 2>/dev/null || true' EXIT
    cp "${KERNEL}"    "${MNT_DIR}/bzImage"
    cp "${INITRAMFS}" "${MNT_DIR}/initramfs.cpio.gz"
    cp "${BOOT_DIR}/extlinux.conf" "${MNT_DIR}/extlinux.conf"
    cp "${BOOT_DIR}/cmdline.conf"  "${MNT_DIR}/cmdline.conf"
    umount "${MNT_DIR}"
    losetup -d "${LOOP_DEV}"
    trap - EXIT
fi

# --- Install extlinux to the partition ---
log "installing extlinux bootloader to partition (offset ${PART_START} sectors)"
# extlinux installs ldlinux.sys into the partition.
extlinux --install --offset="${PART_OFFSET}" "${DISK_IMG}" 2>/dev/null || \
    extlinux --install "${DISK_IMG}" 2>/dev/null || true

# --- Write syslinux MBR to disk ---
# The syslinux MBR code is shipped with syslinux in one of several paths.
MBR_CANDIDATES=(
    /usr/lib/syslinux/mbr/mbr.bin
    /usr/lib/syslinux/mbr.bin
    /usr/share/syslinux/mbr.bin
    /usr/lib/SYSLINUX/mbr.bin
    /usr/lib/syslinux/bios/mbr.bin
)
MBR_SRC=""
for c in "${MBR_CANDIDATES[@]}"; do
    [ -f "$c" ] && { MBR_SRC="$c"; break; }
done

if [ -n "${MBR_SRC}" ]; then
    log "writing syslinux MBR from ${MBR_SRC}"
    dd if="${MBR_SRC}" of="${DISK_IMG}" bs=440 count=1 conv=notrunc 2>/dev/null
else
    log "WARNING: syslinux MBR not found; QEMU must be given -kernel or the disk may not be bootable"
    log "Search paths tried: ${MBR_CANDIDATES[*]}"
fi

# --- Copy menu.c32 for text menu support ---
MENU_CANDIDATES=(
    /usr/lib/syslinux/modules/bios/menu.c32
    /usr/lib/syslinux/menu.c32
    /usr/share/syslinux/menu.c32
)
MENU_SRC=""
for c in "${MENU_CANDIDATES[@]}"; do
    [ -f "$c" ] && { MENU_SRC="$c"; break; }
done

if [ -n "${MENU_SRC}" ] && [ "${HAS_MTOOLS}" -eq 1 ]; then
    log "installing menu.c32"
    mcopy -i "${DISK_IMG}@@${PART_OFFSET}" "${MENU_SRC}" ::/menu.c32 2>/dev/null || true
fi

DISK_SIZE=$(du -sh "${DISK_IMG}" | cut -f1)
log "disk image ready: ${DISK_IMG} (${DISK_SIZE})"
log "boot with: ./scripts/run-qemu.sh --disk ${DISK_IMG}"
