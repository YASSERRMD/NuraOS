#!/usr/bin/env bash
# slot-select.sh -- Read or write the active A/B rootfs slot.
#
# Usage:
#   ./scripts/slot-select.sh               # print active slot (a or b)
#   ./scripts/slot-select.sh set a         # set active slot to 'a'
#   ./scripts/slot-select.sh set b         # set active slot to 'b'
#   ./scripts/slot-select.sh toggle        # switch to the other slot
#   ./scripts/slot-select.sh inactive      # print the inactive slot
#
# Environment:
#   ACTIVE_SLOT_FILE  path to the slot selection file (default: /data/etc/active-slot)
#
# The file contains a single character: 'a' or 'b'.
# The QEMU boot script reads this file to choose the rootfs image.

set -euo pipefail

SLOT_FILE="${ACTIVE_SLOT_FILE:-/data/etc/active-slot}"

log() { printf '[slot-select] %s\n' "$*"; }
die() { log "ERROR: $*" >&2; exit 1; }

get_slot() {
    if [ ! -f "${SLOT_FILE}" ]; then
        echo "a"  # default to slot a when file is absent
        return
    fi
    slot=$(cat "${SLOT_FILE}" | tr -d '[:space:]')
    case "${slot}" in
        a|b) echo "${slot}" ;;
        *) die "corrupt slot file: '${slot}'" ;;
    esac
}

set_slot() {
    local slot="$1"
    case "${slot}" in
        a|b) ;;
        *) die "slot must be 'a' or 'b', got '${slot}'" ;;
    esac
    local dir
    dir=$(dirname "${SLOT_FILE}")
    [ -d "${dir}" ] || mkdir -p "${dir}"
    printf '%s\n' "${slot}" > "${SLOT_FILE}"
    log "active slot set to: ${slot}"
}

toggle_slot() {
    local current
    current=$(get_slot)
    if [ "${current}" = "a" ]; then
        set_slot "b"
    else
        set_slot "a"
    fi
}

CMD="${1:-get}"
shift || true

case "${CMD}" in
    get|"") get_slot ;;
    set)
        [ $# -ge 1 ] || die "'set' requires a slot argument (a or b)"
        set_slot "$1"
        ;;
    toggle) toggle_slot ;;
    inactive)
        current=$(get_slot)
        if [ "${current}" = "a" ]; then echo "b"; else echo "a"; fi
        ;;
    --help|-h)
        grep '^#' "$0" | sed 's/^# \{0,1\}//'
        exit 0
        ;;
    *) die "unknown command: ${CMD}" ;;
esac
