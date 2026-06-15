#!/usr/bin/env bash
# model-activate.sh -- Switch the active model by updating /data/model.json.
#
# Usage:
#   ./scripts/model-activate.sh MODEL_NAME [OPTIONS]
#
# Arguments:
#   MODEL_NAME    Bare name without .gguf extension (must exist in MODEL_DIR)
#
# Options:
#   --model-dir DIR           Directory containing .gguf files (default: /data/models)
#   --manifest PATH           Path to write the manifest (default: /data/model.json)
#   --quantization Q          Quantization label, e.g. Q4_K_M (default: unknown)
#   --context-length N        Context length in tokens (default: 2048)
#   --params-billions N       Parameter count in billions, e.g. 1.7 (default: 0)
#   --architecture ARCH       Architecture name, e.g. smollm2 (default: unknown)
#   --dry-run                 Print the manifest that would be written, do not write
#
# Example:
#   ./scripts/model-activate.sh qwen2-0.5b-instruct-q4_k_m \
#       --quantization Q4_K_M --context-length 2048 --params-billions 0.5 --architecture qwen2

set -euo pipefail

log()  { printf '[model-activate] %s\n' "$*"; }
die()  { log "ERROR: $*" >&2; exit 1; }

MODEL_NAME=""
MODEL_DIR="/data/models"
MANIFEST="/data/model.json"
QUANTIZATION="unknown"
CONTEXT_LENGTH="2048"
PARAMS_BILLIONS="0"
ARCHITECTURE="unknown"
DRY_RUN=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --model-dir)       MODEL_DIR="$2";       shift 2 ;;
        --manifest)        MANIFEST="$2";         shift 2 ;;
        --quantization)    QUANTIZATION="$2";     shift 2 ;;
        --context-length)  CONTEXT_LENGTH="$2";   shift 2 ;;
        --params-billions) PARAMS_BILLIONS="$2";  shift 2 ;;
        --architecture)    ARCHITECTURE="$2";     shift 2 ;;
        --dry-run)         DRY_RUN=1;            shift ;;
        --help|-h)
            grep '^#' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        -*)  die "unknown option: $1" ;;
        *)
            if [ -n "${MODEL_NAME}" ]; then
                die "unexpected argument: $1"
            fi
            MODEL_NAME="$1"
            shift
            ;;
    esac
done

[ -n "${MODEL_NAME}" ] || die "MODEL_NAME is required; run with --help for usage"

GGUF_PATH="${MODEL_DIR}/${MODEL_NAME}.gguf"
[ -f "${GGUF_PATH}" ] || die "model file not found: ${GGUF_PATH}"

SIZE_BYTES=$(wc -c < "${GGUF_PATH}")
SIZE_MB=$(( SIZE_BYTES / 1024 / 1024 ))

MANIFEST_JSON=$(printf '{
  "name": "%s",
  "path": "%s",
  "size_bytes": %s,
  "size_mb": %s,
  "quantization": "%s",
  "context_length": %s,
  "parameters_billions": %s,
  "architecture": "%s"
}' \
    "${MODEL_NAME}" \
    "${GGUF_PATH}" \
    "${SIZE_BYTES}" \
    "${SIZE_MB}" \
    "${QUANTIZATION}" \
    "${CONTEXT_LENGTH}" \
    "${PARAMS_BILLIONS}" \
    "${ARCHITECTURE}")

if [ "${DRY_RUN}" -eq 1 ]; then
    log "dry-run: would write to ${MANIFEST}:"
    printf '%s\n' "${MANIFEST_JSON}"
    exit 0
fi

MANIFEST_DIR=$(dirname "${MANIFEST}")
[ -d "${MANIFEST_DIR}" ] || mkdir -p "${MANIFEST_DIR}"

printf '%s\n' "${MANIFEST_JSON}" > "${MANIFEST}"
log "active model set to: ${MODEL_NAME}"
log "manifest written to: ${MANIFEST}"
log ""
log "Restart nura-agent (or send SIGHUP) to load the new model."
