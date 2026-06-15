#!/usr/bin/env bash
# build-image.sh -- Orchestrate a complete NuraOS image build.
#
# Stages (run in order; each must succeed before the next begins):
#   1. kernel     -- build bzImage via build-kernel.sh
#   2. userland   -- fetch + build musl, BusyBox, nura-agent, gateway, llama-server
#   3. initramfs  -- assemble rootfs and pack initramfs.cpio.gz
#   4. data       -- create the /data ext4 image
#   5. manifest   -- write image/out/manifest.json with sizes, versions, hashes
#
# Outputs (under image/out/):
#   bzImage           Linux kernel image
#   initramfs.cpio.gz Packed initramfs
#   data.img          /data ext4 partition image
#   manifest.json     Versioned build manifest
#
# Usage:
#   ./scripts/build-image.sh [--skip-kernel] [--skip-userland] [--help]
#
# Environment overrides (see scripts/VERSIONS.env for defaults):
#   NURA_VERSION, KERNEL_VERSION, RUST_VERSION, GO_VERSION, etc.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUT_DIR="${REPO_ROOT}/image/out"

# Source version lock.
# shellcheck source=VERSIONS.env
source "${SCRIPT_DIR}/VERSIONS.env"

SKIP_KERNEL=0
SKIP_USERLAND=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --skip-kernel)   SKIP_KERNEL=1 ;;
        --skip-userland) SKIP_USERLAND=1 ;;
        --help|-h)
            grep '^#' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) echo "[build-image] unknown argument: $1" >&2; exit 1 ;;
    esac
    shift
done

# ----- Utilities -----

BUILD_START=$(date +%s)
STAGE_TIMES=()

log()  { printf '[build-image] %s\n' "$*"; }
err()  { printf '[build-image] ERROR: %s\n' "$*" >&2; }
die()  { err "$*"; exit 1; }

stage() {
    local name="$1"
    local start
    start=$(date +%s)
    log "=== STAGE: ${name} ==="
    STAGE_TIMES+=("${name}:${start}")
}

stage_done() {
    local name="$1"
    local end
    end=$(date +%s)
    # Find start time for this stage.
    for entry in "${STAGE_TIMES[@]}"; do
        if [[ "${entry%%:*}" == "${name}" ]]; then
            local s="${entry##*:}"
            log "=== DONE: ${name} ($((end - s))s) ==="
            return
        fi
    done
    log "=== DONE: ${name} ==="
}

run_stage() {
    local name="$1"
    shift
    stage "${name}"
    if ! "$@"; then
        die "stage '${name}' failed (command: $*)"
    fi
    stage_done "${name}"
}

sha256sum_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}

file_size_bytes() {
    stat -c %s "$1" 2>/dev/null || stat -f %z "$1" 2>/dev/null || echo 0
}

# ----- Prerequisites -----

mkdir -p "${OUT_DIR}"

command -v go    >/dev/null 2>&1 || die "go not found; install Go ${GO_VERSION}"
command -v cargo >/dev/null 2>&1 || die "cargo not found; install Rust ${RUST_VERSION}"

# ----- Stage 1: Kernel -----

if [ "${SKIP_KERNEL}" -eq 0 ]; then
    run_stage "kernel" bash "${SCRIPT_DIR}/build-kernel.sh"
else
    log "skipping kernel (--skip-kernel)"
    [ -f "${OUT_DIR}/bzImage" ] || die "bzImage not found and --skip-kernel was set"
fi

# ----- Stage 2: Userland -----

if [ "${SKIP_USERLAND}" -eq 0 ]; then
    stage "userland"

    log "building nura-agent ..."
    bash "${SCRIPT_DIR}/build-agent.sh"

    log "building gateway ..."
    bash "${SCRIPT_DIR}/build-gateway.sh"

    # llama-server is optional; skip if not fetched.
    if [ -d "${REPO_ROOT}/vendor/llama.cpp" ] || [ -d "${REPO_ROOT}/../llama.cpp" ]; then
        log "building llama-server ..."
        bash "${SCRIPT_DIR}/build-llama.sh" || log "WARNING: llama-server build skipped"
    else
        log "llama-server sources not found; skipping (run fetch-llama.sh first)"
    fi

    stage_done "userland"
else
    log "skipping userland (--skip-userland)"
fi

# ----- Stage 3: Initramfs -----

run_stage "initramfs" bash "${SCRIPT_DIR}/build-initramfs.sh"

# ----- Stage 4: /data image -----

run_stage "data-image" bash "${SCRIPT_DIR}/make-data-image.sh"

# ----- Stage 5: Manifest -----

stage "manifest"

MANIFEST="${OUT_DIR}/manifest.json"
BUILD_END=$(date +%s)
BUILD_DURATION=$((BUILD_END - BUILD_START))

# Collect artifact sizes and hashes.
collect_artifact() {
    local path="$1"
    local name="$2"
    if [ -f "${path}" ]; then
        local size hash
        size=$(file_size_bytes "${path}")
        hash=$(sha256sum_file "${path}")
        printf '    {"%s": {"path": "%s", "size_bytes": %s, "sha256": "%s"}}' \
            "${name}" "${path}" "${size}" "${hash}"
    else
        printf '    {"%s": null}' "${name}"
    fi
}

# Build JSON manifest.
cat > "${MANIFEST}" <<MANIFEST_EOF
{
  "nura_version": "${NURA_VERSION}",
  "kernel_version": "${KERNEL_VERSION}",
  "musl_version": "${MUSL_VERSION}",
  "busybox_version": "${BUSYBOX_VERSION}",
  "rust_version": "${RUST_VERSION}",
  "go_version": "${GO_VERSION}",
  "build_duration_seconds": ${BUILD_DURATION},
  "artifacts": {
    "bzImage": $(
        f="${OUT_DIR}/bzImage"
        if [ -f "${f}" ]; then
            printf '{"path": "%s", "size_bytes": %s, "sha256": "%s"}' \
                "${f}" "$(file_size_bytes "${f}")" "$(sha256sum_file "${f}")"
        else
            echo 'null'
        fi
    ),
    "initramfs": $(
        f="${OUT_DIR}/initramfs.cpio.gz"
        if [ -f "${f}" ]; then
            printf '{"path": "%s", "size_bytes": %s, "sha256": "%s"}' \
                "${f}" "$(file_size_bytes "${f}")" "$(sha256sum_file "${f}")"
        else
            echo 'null'
        fi
    ),
    "data_img": $(
        f="${OUT_DIR}/data.img"
        if [ -f "${f}" ]; then
            printf '{"path": "%s", "size_bytes": %s, "sha256": "%s"}' \
                "${f}" "$(file_size_bytes "${f}")" "$(sha256sum_file "${f}")"
        else
            echo 'null'
        fi
    )
  }
}
MANIFEST_EOF

log "manifest written to ${MANIFEST}"
stage_done "manifest"

# ----- Summary -----

log ""
log "Build complete in ${BUILD_DURATION}s"
log "Output directory: ${OUT_DIR}"
log ""
for artifact in bzImage initramfs.cpio.gz data.img manifest.json; do
    f="${OUT_DIR}/${artifact}"
    if [ -f "${f}" ]; then
        size=$(file_size_bytes "${f}")
        log "  ${artifact}: ${size} bytes"
    fi
done
log ""
log "Boot with: ./scripts/run-qemu.sh"
