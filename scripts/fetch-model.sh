#!/usr/bin/env bash
# Download a quantised GGUF model into /data/models and write /data/model.json.
#
# Usage:
#   bash scripts/fetch-model.sh [--model-dir DIR] [--model-url URL]
#                               [--model-name NAME] [--dry-run]
#
# Defaults to SmolLM2-1.7B-Instruct Q4_K_M (~1 GB) which fits comfortably
# in 2 GB RAM including the OS and agent overhead.
#
# MODEL_URL and MODEL_NAME env vars override the defaults.
set -euo pipefail

# -- defaults (overridable via env) --
DEFAULT_MODEL_NAME="smollm2-1.7b-instruct-q4_k_m"
DEFAULT_MODEL_URL="https://huggingface.co/bartowski/SmolLM2-1.7B-Instruct-GGUF/resolve/main/SmolLM2-1.7B-Instruct-Q4_K_M.gguf"

MODEL_NAME="${MODEL_NAME:-${DEFAULT_MODEL_NAME}}"
MODEL_URL="${MODEL_URL:-${DEFAULT_MODEL_URL}}"
MODEL_DIR="${MODEL_DIR:-/data/models}"
MANIFEST="${MODEL_DIR%/}/../model.json"
DRY_RUN=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --model-dir)  MODEL_DIR="$2"; MANIFEST="${MODEL_DIR%/}/../model.json"; shift 2 ;;
        --model-url)  MODEL_URL="$2"; shift 2 ;;
        --model-name) MODEL_NAME="$2"; shift 2 ;;
        --dry-run)    DRY_RUN=1; shift ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

DEST_FILE="${MODEL_DIR}/${MODEL_NAME}.gguf"

echo "Model:    ${MODEL_NAME}"
echo "URL:      ${MODEL_URL}"
echo "Dest:     ${DEST_FILE}"
echo "Manifest: ${MANIFEST}"

if [ "${DRY_RUN}" -eq 1 ]; then
    echo "[dry-run] would download and write manifest"
    exit 0
fi

mkdir -p "${MODEL_DIR}"

if [ -f "${DEST_FILE}" ]; then
    echo "Model already present at ${DEST_FILE} -- skipping download"
else
    echo "Downloading model (this may take a while) ..."
    PARTIAL="${DEST_FILE}.part"
    curl -L --fail --retry 3 --retry-delay 5 --progress-bar \
         -o "${PARTIAL}" "${MODEL_URL}"
    mv "${PARTIAL}" "${DEST_FILE}"
    echo "Download complete: ${DEST_FILE}"
fi

SIZE_BYTES="$(wc -c < "${DEST_FILE}")"
SIZE_MB="$(( SIZE_BYTES / 1024 / 1024 ))"

# Write the model manifest consumed by the agent and llama-server wrapper.
cat > "${MANIFEST}" <<EOF
{
  "name":                "${MODEL_NAME}",
  "path":                "${DEST_FILE}",
  "size_bytes":          ${SIZE_BYTES},
  "size_mb":             ${SIZE_MB},
  "quantization":        "Q4_K_M",
  "context_length":      2048,
  "parameters_billions": 1.7,
  "architecture":        "smollm2"
}
EOF

echo "Manifest written: ${MANIFEST}"
echo "Model size: ${SIZE_MB} MB"
