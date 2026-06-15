#!/usr/bin/env bash
# update.sh -- Download and install a NuraOS rootfs update into the inactive slot.
#
# Usage:
#   ./scripts/update.sh [OPTIONS]
#
# Options:
#   --url URL          URL of the rootfs image (.ext4 or .img); required unless --local is set
#   --local PATH       Use a local image file instead of downloading
#   --sha256 HASH      Expected SHA-256 of the image (optional; skipped if absent)
#   --rootfs-dir DIR   Directory containing rootfs-a.ext4 and rootfs-b.ext4 (default: /boot)
#   --state-file PATH  Update state JSON file (default: /data/etc/update-state.json)
#   --dry-run          Validate inputs, print the plan, and exit without writing
#   --rollback         Mark the current inactive slot as active (undo last pending update)
#   --status           Print the current update state and exit
#
# Exit codes:
#   0  success
#   1  error (see stderr)
#   2  already up to date (no write performed)
#
# The update state file records:
#   active_slot, pending_slot, last_update, last_result, boot_attempts

set -euo pipefail

SLOT_FILE="${ACTIVE_SLOT_FILE:-/data/etc/active-slot}"
ROOTFS_DIR="${1+$1}"   # will be parsed below
STATE_FILE="/data/etc/update-state.json"
IMAGE_URL=""
LOCAL_IMAGE=""
EXPECTED_SHA256=""
ROOTFS_DIR_ARG="/boot"
DRY_RUN=0
ROLLBACK=0
SHOW_STATUS=0

log()  { printf '[update] %s\n' "$*"; }
die()  { log "ERROR: $*" >&2; exit 1; }

while [[ $# -gt 0 ]]; do
    case "$1" in
        --url)        IMAGE_URL="$2";      shift 2 ;;
        --local)      LOCAL_IMAGE="$2";    shift 2 ;;
        --sha256)     EXPECTED_SHA256="$2"; shift 2 ;;
        --rootfs-dir) ROOTFS_DIR_ARG="$2"; shift 2 ;;
        --state-file) STATE_FILE="$2";     shift 2 ;;
        --dry-run)    DRY_RUN=1;           shift ;;
        --rollback)   ROLLBACK=1;          shift ;;
        --status)     SHOW_STATUS=1;       shift ;;
        --help|-h)
            grep '^#' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) die "unknown argument: $1" ;;
    esac
done

# ---- helpers ----

get_slot() {
    if [ -f "${SLOT_FILE}" ]; then
        cat "${SLOT_FILE}" | tr -d '[:space:]'
    else
        echo "a"
    fi
}

set_slot() {
    local dir; dir=$(dirname "${SLOT_FILE}")
    [ -d "${dir}" ] || mkdir -p "${dir}"
    printf '%s\n' "$1" > "${SLOT_FILE}"
}

write_state() {
    local active="$1" pending="$2" result="$3" attempts="$4"
    local dir; dir=$(dirname "${STATE_FILE}")
    [ -d "${dir}" ] || mkdir -p "${dir}"
    local ts; ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "unknown")
    cat > "${STATE_FILE}" <<STATEJSON
{
  "active_slot": "${active}",
  "pending_slot": ${pending},
  "last_update": "${ts}",
  "last_result": "${result}",
  "boot_attempts": ${attempts}
}
STATEJSON
}

read_state_field() {
    local field="$1"
    if [ -f "${STATE_FILE}" ]; then
        grep "\"${field}\"" "${STATE_FILE}" | sed 's/.*"[^"]*":[[:space:]]*\(.*\),\{0,1\}/\1/' | tr -d '"' | tr -d '[:space:]'
    fi
}

# ---- status mode ----

if [ "${SHOW_STATUS}" -eq 1 ]; then
    active=$(get_slot)
    if [ "${active}" = "a" ]; then inactive="b"; else inactive="a"; fi
    log "active slot:   ${active}"
    log "inactive slot: ${inactive}"
    if [ -f "${STATE_FILE}" ]; then
        log "state file:    ${STATE_FILE}"
        cat "${STATE_FILE}"
    else
        log "state file:    not found (no update performed yet)"
    fi
    exit 0
fi

# ---- rollback mode ----

if [ "${ROLLBACK}" -eq 1 ]; then
    active=$(get_slot)
    if [ "${active}" = "a" ]; then new_active="b"; else new_active="a"; fi
    log "rolling back from slot ${active} to slot ${new_active}"
    if [ "${DRY_RUN}" -eq 1 ]; then
        log "dry-run: would set active slot to ${new_active}"
        exit 0
    fi
    set_slot "${new_active}"
    write_state "${new_active}" "null" "rolled_back" "0"
    log "rollback complete; reboot to activate slot ${new_active}"
    exit 0
fi

# ---- update mode ----

[ -n "${IMAGE_URL}" ] || [ -n "${LOCAL_IMAGE}" ] || \
    die "--url or --local is required; run with --help for usage"

active=$(get_slot)
if [ "${active}" = "a" ]; then
    target_slot="b"
else
    target_slot="a"
fi
target_image="${ROOTFS_DIR_ARG}/rootfs-${target_slot}.ext4"

log "active slot:  ${active}"
log "target slot:  ${target_slot}"
log "target image: ${target_image}"

if [ "${DRY_RUN}" -eq 1 ]; then
    log "dry-run: would write to ${target_image}"
    [ -n "${IMAGE_URL}" ] && log "dry-run: would download from ${IMAGE_URL}"
    [ -n "${LOCAL_IMAGE}" ] && log "dry-run: would copy from ${LOCAL_IMAGE}"
    [ -n "${EXPECTED_SHA256}" ] && log "dry-run: would verify sha256=${EXPECTED_SHA256}"
    exit 0
fi

# Download or copy the image to a temp file first, then verify, then install.
TMPDIR_WORK=$(mktemp -d /tmp/nura-update.XXXXXX)
trap 'rm -rf "${TMPDIR_WORK}"' EXIT

STAGING="${TMPDIR_WORK}/rootfs-staging.ext4"

if [ -n "${LOCAL_IMAGE}" ]; then
    log "copying local image: ${LOCAL_IMAGE}"
    [ -f "${LOCAL_IMAGE}" ] || die "local image not found: ${LOCAL_IMAGE}"
    cp "${LOCAL_IMAGE}" "${STAGING}"
else
    log "downloading: ${IMAGE_URL}"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "${STAGING}" "${IMAGE_URL}"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O "${STAGING}" "${IMAGE_URL}"
    else
        die "curl or wget required for download"
    fi
fi

if [ -n "${EXPECTED_SHA256}" ]; then
    log "verifying sha256..."
    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "${STAGING}" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        actual=$(shasum -a 256 "${STAGING}" | awk '{print $1}')
    else
        die "sha256sum or shasum required for integrity check"
    fi
    if [ "${actual}" != "${EXPECTED_SHA256}" ]; then
        die "sha256 mismatch: expected=${EXPECTED_SHA256} actual=${actual}"
    fi
    log "sha256 OK: ${actual}"
fi

log "installing to ${target_image}"
TARGET_DIR=$(dirname "${target_image}")
[ -d "${TARGET_DIR}" ] || mkdir -p "${TARGET_DIR}"
cp "${STAGING}" "${target_image}"

write_state "${active}" "\"${target_slot}\"" "pending_reboot" "0"
log "update staged to slot ${target_slot}"
log "run: bash scripts/slot-select.sh set ${target_slot} && reboot"
