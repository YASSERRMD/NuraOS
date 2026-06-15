#!/usr/bin/env bash
# board-info.sh -- Detect or display hardware board information.
#
# Usage:
#   ./scripts/board-info.sh [OPTIONS]
#
# Options:
#   --detect          Auto-detect board from /proc/cpuinfo and write board.json
#   --board-id ID     Print the named board config from boards/ and exit
#   --list            List all known board IDs and exit
#   --board-info PATH Path to the runtime board info file (default: /data/etc/board.json)
#   --boards-dir DIR  Directory containing board JSON configs (default: boards/)
#   --write           Write the selected or detected board to --board-info path
#
# Without flags, reads and prints the current board info file.

set -euo pipefail

BOARD_INFO_FILE="${BOARD_INFO_FILE:-/data/etc/board.json}"
BOARDS_DIR="$(dirname "$0")/../boards"
DETECT=0
LIST=0
BOARD_ID=""
WRITE=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --detect)       DETECT=1;             shift ;;
        --board-id)     BOARD_ID="$2";        shift 2 ;;
        --list)         LIST=1;               shift ;;
        --board-info)   BOARD_INFO_FILE="$2"; shift 2 ;;
        --boards-dir)   BOARDS_DIR="$2";      shift 2 ;;
        --write)        WRITE=1;              shift ;;
        --help|-h)
            grep '^#' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) echo "[board-info] unknown argument: $1" >&2; exit 1 ;;
    esac
done

log() { printf '[board-info] %s\n' "$*"; }

# ---- list mode ----
if [ "${LIST}" -eq 1 ]; then
    log "Known board IDs:"
    for f in "${BOARDS_DIR}"/*.json; do
        id=$(basename "${f}" .json)
        name=$(grep '"name"' "${f}" | sed 's/.*"name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
        printf '  %-20s  %s\n' "${id}" "${name}"
    done
    exit 0
fi

# ---- detect mode ----
if [ "${DETECT}" -eq 1 ]; then
    ARCH=$(uname -m)
    BOARD_GUESS="qemu-x86_64"
    case "${ARCH}" in
        x86_64)
            # Check for QEMU guest via DMI (if available)
            if [ -r /sys/class/dmi/id/product_name ]; then
                prod=$(cat /sys/class/dmi/id/product_name 2>/dev/null || echo "")
                if echo "${prod}" | grep -qi "standard pc\|qemu"; then
                    BOARD_GUESS="qemu-x86_64"
                fi
            fi
            ;;
        aarch64|arm64)
            model=""
            if [ -r /proc/device-tree/model ]; then
                model=$(cat /proc/device-tree/model 2>/dev/null | tr -d '\0' || echo "")
            fi
            if echo "${model}" | grep -qi "raspberry pi 5"; then
                BOARD_GUESS="rpi5"
            elif echo "${model}" | grep -qi "raspberry pi 4"; then
                BOARD_GUESS="rpi4"
            else
                BOARD_GUESS="generic-arm64"
            fi
            ;;
    esac
    log "detected board: ${BOARD_GUESS}"
    BOARD_ID="${BOARD_GUESS}"
fi

# ---- named board mode ----
if [ -n "${BOARD_ID}" ]; then
    BOARD_FILE="${BOARDS_DIR}/${BOARD_ID}.json"
    [ -f "${BOARD_FILE}" ] || { log "ERROR: unknown board '${BOARD_ID}'"; exit 1; }
    if [ "${WRITE}" -eq 1 ]; then
        BOARD_INFO_DIR=$(dirname "${BOARD_INFO_FILE}")
        [ -d "${BOARD_INFO_DIR}" ] || mkdir -p "${BOARD_INFO_DIR}"
        cp "${BOARD_FILE}" "${BOARD_INFO_FILE}"
        log "board info written to ${BOARD_INFO_FILE}"
    fi
    cat "${BOARD_FILE}"
    exit 0
fi

# ---- default: read current board info file ----
if [ -f "${BOARD_INFO_FILE}" ]; then
    cat "${BOARD_INFO_FILE}"
else
    log "No board info file at ${BOARD_INFO_FILE}"
    log "Run: ./scripts/board-info.sh --detect --write"
    log "  or: ./scripts/board-info.sh --board-id qemu-x86_64 --write"
    exit 1
fi
