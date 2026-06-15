#!/usr/bin/env bash
# model-list.sh -- List available GGUF models and show the active model.
#
# Usage:
#   ./scripts/model-list.sh [--model-dir DIR] [--manifest PATH]
#
# Options:
#   --model-dir DIR    Directory to scan for .gguf files (default: /data/models)
#   --manifest PATH    Path to the active model manifest (default: /data/model.json)

set -euo pipefail

MODEL_DIR="/data/models"
MANIFEST="/data/model.json"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --model-dir)  MODEL_DIR="$2"; shift 2 ;;
        --manifest)   MANIFEST="$2"; shift 2 ;;
        --help|-h)
            grep '^#' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) echo "[model-list] unknown argument: $1" >&2; exit 1 ;;
    esac
    shift
done

log() { printf '[model-list] %s\n' "$*"; }

# ---- Active model ----
ACTIVE_NAME=""
ACTIVE_PATH=""
if [ -f "${MANIFEST}" ]; then
    ACTIVE_NAME=$(grep '"name"' "${MANIFEST}" | sed 's/.*"name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/' | head -1)
    ACTIVE_PATH=$(grep '"path"' "${MANIFEST}" | sed 's/.*"path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/' | head -1)
    log "Active model: ${ACTIVE_NAME}"
    log "Active path:  ${ACTIVE_PATH}"
else
    log "No manifest found at ${MANIFEST} (no active model)"
fi

log ""
log "Available models in ${MODEL_DIR}:"
log ""

if [ ! -d "${MODEL_DIR}" ]; then
    log "  (directory not found)"
    exit 0
fi

COUNT=0
while IFS= read -r -d '' gguf; do
    name=$(basename "${gguf}" .gguf)
    size=$(wc -c < "${gguf}" 2>/dev/null || echo 0)
    size_mb=$(( size / 1024 / 1024 ))
    if [ "${name}" = "${ACTIVE_NAME}" ]; then
        printf '  * %-50s  %6d MB  (active)\n' "${name}" "${size_mb}"
    else
        printf '    %-50s  %6d MB\n' "${name}" "${size_mb}"
    fi
    COUNT=$(( COUNT + 1 ))
done < <(find "${MODEL_DIR}" -maxdepth 1 -name '*.gguf' -print0 | sort -z)

if [ "${COUNT}" -eq 0 ]; then
    log "  (no .gguf files found)"
    log ""
    log "Download a model with: ./scripts/fetch-model.sh"
fi
