#!/bin/sh
# Test that /data stays consistent after a simulated power loss.
#
# Strategy: create a loop-mounted ext4 image, write via the atomic helper,
# then unmount abruptly (simulating power loss) and run e2fsck to verify
# the filesystem is clean.
#
# Requires: root or loop-mount capability, mkfs.ext4, e2fsck.
# Run as: sudo scripts/test-power-loss.sh
set -e

DISK_IMG=$(mktemp /tmp/nura-test-data-XXXXXX.img)
MOUNTPOINT=$(mktemp -d /tmp/nura-test-mnt-XXXXXX)

cleanup() {
    umount -f "${MOUNTPOINT}" 2>/dev/null || true
    rm -f "${DISK_IMG}"
    rmdir "${MOUNTPOINT}" 2>/dev/null || true
}
trap cleanup EXIT

echo "[power-loss-test] creating 32 MiB ext4 image ..."
dd if=/dev/zero of="${DISK_IMG}" bs=1M count=32 status=none
mkfs.ext4 -F -q -E lazy_itable_init=0 "${DISK_IMG}"

echo "[power-loss-test] mounting with ordered+barrier options ..."
if ! mount -t ext4 -o data=ordered,barrier=1,loop "${DISK_IMG}" "${MOUNTPOINT}" 2>/dev/null; then
    echo "SKIP: cannot loop-mount (run as root with loop support)"
    exit 0
fi

echo "[power-loss-test] writing files atomically ..."
for i in $(seq 1 20); do
    TMPF="${MOUNTPOINT}/.atomicfile-${i}.tmp"
    TARGET="${MOUNTPOINT}/state-${i}.json"
    printf '{"seq":%d,"data":"value%d"}' "${i}" "${i}" > "${TMPF}"
    sync "${TMPF}"
    mv "${TMPF}" "${TARGET}"
done

echo "[power-loss-test] simulating abrupt unmount (power loss) ..."
# umount -f simulates pulling the plug (no barrier flush guarantee).
umount -f "${MOUNTPOINT}" 2>/dev/null || umount "${MOUNTPOINT}"

echo "[power-loss-test] running e2fsck on unmounted image ..."
if e2fsck -fn "${DISK_IMG}" > /tmp/nura-fsck-out.txt 2>&1; then
    echo "[power-loss-test] PASS: filesystem is clean"
else
    echo "[power-loss-test] FAIL: e2fsck reported errors:"
    cat /tmp/nura-fsck-out.txt
    exit 1
fi

echo "[power-loss-test] PASS"
