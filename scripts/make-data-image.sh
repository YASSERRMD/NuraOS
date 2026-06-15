#!/usr/bin/env bash
# Create an ext4 image for the /data persistent partition.
#
# The image is written to image/out/data.img. It contains the following
# directory structure:
#   /data/models     GGUF model files (populated separately, gitignored)
#   /data/logs       boot logs, agent logs, rotation targets
#   /data/sessions   per-session provenance JSONL files
#   /data/etc        agent.toml, secrets.toml (never committed)
#
# Usage: ./scripts/make-data-image.sh [--size MB]
#   --size MB  size of the image in megabytes (default: 2048)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUT_DIR="${REPO_ROOT}/image/out"
DATA_IMG="${OUT_DIR}/data.img"
SIZE_MB=2048

while [[ $# -gt 0 ]]; do
    case "$1" in
        --size) shift; SIZE_MB="$1" ;;
        *) echo "unknown argument: $1" >&2; exit 1 ;;
    esac
    shift
done

log() { printf '[make-data-image] %s\n' "$*"; }
die() { printf '[make-data-image] ERROR: %s\n' "$*" >&2; exit 1; }

for tool in dd mkfs.ext4; do
    command -v "${tool}" >/dev/null 2>&1 || die "required tool not found: ${tool}"
done

mkdir -p "${OUT_DIR}"

log "creating ${SIZE_MB} MB ext4 image at ${DATA_IMG} ..."
dd if=/dev/zero of="${DATA_IMG}" bs=1M count="${SIZE_MB}" status=progress 2>&1

log "formatting as ext4 ..."
mkfs.ext4 -L nura-data -F \
    -E lazy_itable_init=0,lazy_journal_init=0 \
    "${DATA_IMG}"

# Populate directory structure using e2tools or a mount loop if available.
# Prefer e2mkdir (from e2tools) over sudo mount for CI compatibility.
if command -v e2mkdir >/dev/null 2>&1; then
    log "creating directory layout (e2mkdir) ..."
    for subdir in models logs sessions etc; do
        e2mkdir "${DATA_IMG}:${subdir}"
    done
elif command -v debugfs >/dev/null 2>&1; then
    log "creating directory layout (debugfs) ..."
    debugfs -w "${DATA_IMG}" <<'EOF' 2>/dev/null
mkdir models
mkdir logs
mkdir sessions
mkdir etc
EOF
else
    log "WARNING: e2tools and debugfs not found; directory layout will be created by /init at first boot"
fi

SIZE=$(du -h "${DATA_IMG}" | cut -f1)
log "data image: ${DATA_IMG} (${SIZE})"
log "attach with: ./scripts/run-qemu.sh (data image is picked up automatically)"
log "done."
