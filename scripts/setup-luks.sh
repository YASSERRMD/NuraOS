#!/bin/sh
# Set up LUKS encryption for the NuraOS /data disk image.
#
# Run this on the HOST machine (outside QEMU) before first boot.
# The resulting image will require unlocking at boot via a key file or passphrase.
#
# Prerequisites (host):
#   cryptsetup  -- LUKS userspace tool
#   mkfs.ext4   -- ext4 formatter
#
# Usage:
#   sudo ./scripts/setup-luks.sh [--image PATH] [--key-file PATH] [--size MB]
#
# Options:
#   --image PATH      Path to the data disk image (default: image/out/data.img)
#   --key-file PATH   Path to a binary key file (4096 bytes, created if absent)
#   --size MB         Image size in MiB when creating a new image (default: 2048)
#
# Example (passphrase only):
#   sudo ./scripts/setup-luks.sh
#
# Example (key file on a separate image):
#   dd if=/dev/urandom bs=4096 count=1 of=key.bin
#   sudo ./scripts/setup-luks.sh --key-file key.bin
#
# After setup, boot QEMU with:
#   QEMU_EXTRA="-drive file=key.bin,format=raw,if=virtio" ./scripts/run-qemu.sh
#   # and add nura.data.luks=1 to the kernel cmdline in run-qemu.sh

set -e

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DATA_IMG="${REPO_ROOT}/image/out/data.img"
KEY_FILE=""
SIZE_MB=2048
MAPPER_NAME="nura-data-setup"

die()  { echo "setup-luks: FATAL: $*" >&2; exit 1; }
log()  { echo "setup-luks: $*"; }
warn() { echo "setup-luks: WARNING: $*" >&2; }

# Parse arguments.
while [ $# -gt 0 ]; do
    case "$1" in
        --image)    shift; DATA_IMG="$1" ;;
        --key-file) shift; KEY_FILE="$1" ;;
        --size)     shift; SIZE_MB="$1"  ;;
        *) die "unknown argument: $1" ;;
    esac
    shift
done

# Require root.
[ "$(id -u)" -eq 0 ] || die "must run as root (sudo)"

# Require cryptsetup.
command -v cryptsetup >/dev/null 2>&1 || die "cryptsetup not found; install it first"
command -v mkfs.ext4  >/dev/null 2>&1 || die "mkfs.ext4 not found; install e2fsprogs"

# Create image if absent.
if [ ! -f "${DATA_IMG}" ]; then
    log "creating ${SIZE_MB} MiB data image at ${DATA_IMG} ..."
    mkdir -p "$(dirname "${DATA_IMG}")"
    dd if=/dev/zero of="${DATA_IMG}" bs=1M count="${SIZE_MB}" status=progress
fi

log "data image: ${DATA_IMG}"

# Generate key file if requested but absent.
if [ -n "${KEY_FILE}" ] && [ ! -f "${KEY_FILE}" ]; then
    log "generating 4096-byte key file at ${KEY_FILE} ..."
    dd if=/dev/urandom of="${KEY_FILE}" bs=4096 count=1 status=none
    chmod 600 "${KEY_FILE}"
    log "key file created; keep it safe -- losing it means losing your data"
fi

# Format with LUKS2.
log "formatting ${DATA_IMG} with LUKS2 ..."
if [ -n "${KEY_FILE}" ]; then
    cryptsetup luksFormat --type luks2 --batch-mode \
        --key-file "${KEY_FILE}" --keyfile-size 4096 \
        "${DATA_IMG}"
    log "LUKS2 formatted with key file"
else
    cryptsetup luksFormat --type luks2 --batch-mode "${DATA_IMG}"
    log "LUKS2 formatted with passphrase"
fi

# Optionally add the key file as an additional slot so passphrase also works.
if [ -n "${KEY_FILE}" ]; then
    log "adding a passphrase slot (slot 1) for manual recovery ..."
    log "(leave blank to skip; you can add it later with cryptsetup luksAddKey)"
    cryptsetup luksAddKey --key-file "${KEY_FILE}" --keyfile-size 4096 \
        "${DATA_IMG}" || warn "passphrase slot not added (skipped or cancelled)"
fi

# Open the container.
log "opening LUKS container as /dev/mapper/${MAPPER_NAME} ..."
if [ -n "${KEY_FILE}" ]; then
    cryptsetup luksOpen --key-file "${KEY_FILE}" --keyfile-size 4096 \
        "${DATA_IMG}" "${MAPPER_NAME}"
else
    cryptsetup luksOpen "${DATA_IMG}" "${MAPPER_NAME}"
fi

# Format the inner ext4 filesystem.
log "creating ext4 filesystem inside the container ..."
mkfs.ext4 -F -q -E lazy_itable_init=0 "/dev/mapper/${MAPPER_NAME}"
log "ext4 created"

# Create the expected subdirectories.
MOUNT_TMP="$(mktemp -d)"
mount "/dev/mapper/${MAPPER_NAME}" "${MOUNT_TMP}"
mkdir -p "${MOUNT_TMP}/models" "${MOUNT_TMP}/logs" \
         "${MOUNT_TMP}/sessions" "${MOUNT_TMP}/etc" "${MOUNT_TMP}/journal"
umount "${MOUNT_TMP}"
rmdir "${MOUNT_TMP}"

# Close the container.
cryptsetup luksClose "${MAPPER_NAME}"

log "LUKS setup complete."
log ""
log "To boot NuraOS with encryption, pass on the kernel cmdline:"
log "  nura.data.luks=1"
if [ -n "${KEY_FILE}" ]; then
    log ""
    log "For automatic unlock attach the key file as a second virtio disk:"
    log "  QEMU_EXTRA=\"-drive file=${KEY_FILE},format=raw,if=virtio\" ./scripts/run-qemu.sh"
fi
log ""
log "Without a key device the agent will prompt for the passphrase on the console."
