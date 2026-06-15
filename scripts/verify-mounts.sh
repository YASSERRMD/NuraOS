#!/usr/bin/env bash
# Verify the NuraOS runtime mount layout matches the expected ephemeral/persistent split.
#
# Run this inside a live NuraOS VM or after booting the QEMU image:
#   ./scripts/run-qemu.sh
#   # from inside the VM:
#   sh /scripts/verify-mounts.sh

set -e
PASS=0
FAIL=0

check_mount() {
    # check_mount <path> <expected-fstype> <label>
    local path="$1" fstype="$2" label="$3"
    local actual
    actual=$(grep " ${path} " /proc/mounts 2>/dev/null | awk '{print $3}' | head -1)
    if [ "${actual}" = "${fstype}" ]; then
        echo "  PASS: ${label} (${path} is ${fstype})"
        PASS=$((PASS+1))
    else
        echo "  FAIL: ${label} -- ${path}: expected ${fstype}, got '${actual}'"
        FAIL=$((FAIL+1))
    fi
}

check_exists() {
    local path="$1" label="$2"
    if [ -e "${path}" ]; then
        echo "  PASS: ${label} (${path} exists)"
        PASS=$((PASS+1))
    else
        echo "  FAIL: ${label} -- ${path} missing"
        FAIL=$((FAIL+1))
    fi
}

check_symlink() {
    local path="$1" target="$2" label="$3"
    local actual
    actual=$(readlink "${path}" 2>/dev/null)
    if [ "${actual}" = "${target}" ]; then
        echo "  PASS: ${label} (${path} -> ${target})"
        PASS=$((PASS+1))
    else
        echo "  FAIL: ${label} -- ${path} -> '${actual}', expected '${target}'"
        FAIL=$((FAIL+1))
    fi
}

echo "=== NuraOS mount layout verification ==="
echo ""
echo "-- Ephemeral tmpfs mounts --"
check_mount /tmp  tmpfs "/tmp is ephemeral tmpfs"
check_mount /run  tmpfs "/run is ephemeral tmpfs"
check_mount /var  tmpfs "/var is ephemeral tmpfs"

echo ""
echo "-- FHS compatibility --"
check_symlink /var/run /run "/var/run -> /run symlink"

echo ""
echo "-- Persistent partition --"
if grep -q " /data " /proc/mounts 2>/dev/null; then
    DATA_FS=$(grep " /data " /proc/mounts | awk '{print $3}' | head -1)
    echo "  INFO: /data is mounted (fstype=${DATA_FS})"
    PASS=$((PASS+1))
else
    echo "  FAIL: /data is not mounted"
    FAIL=$((FAIL+1))
fi
check_exists /data/etc   "/data/etc directory exists"
check_exists /data/journal "/data/journal directory exists"

echo ""
echo "-- OS bins are in initramfs --"
check_exists /bin/sh   "/bin/sh exists"
check_exists /sbin/mount "/sbin/mount exists"

echo ""
echo "=== Results: ${PASS} passed, ${FAIL} failed ==="
[ "${FAIL}" -eq 0 ]
