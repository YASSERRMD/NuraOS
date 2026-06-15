#!/usr/bin/env bash
# package-release.sh -- Bundle the NuraOS image artifacts into a versioned
# release archive.
#
# Must be run after scripts/build-image.sh has produced image/out/.
#
# Outputs:
#   dist/nuraos-<version>.tar.gz        Release archive
#   dist/nuraos-<version>.tar.gz.sha256 SHA-256 checksum file
#   dist/nuraos-<version>-manifest.json Copy of the build manifest
#
# Usage:
#   ./scripts/package-release.sh [--version VERSION] [--out DIR]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUT_DIR="${REPO_ROOT}/image/out"
DIST_DIR="${REPO_ROOT}/dist"

# shellcheck source=VERSIONS.env
source "${SCRIPT_DIR}/VERSIONS.env"

VERSION="${NURA_VERSION}"
CUSTOM_OUT=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --version) shift; VERSION="$1" ;;
        --out)     shift; CUSTOM_OUT="$1" ;;
        --help|-h)
            grep '^#' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) echo "[package-release] unknown argument: $1" >&2; exit 1 ;;
    esac
    shift
done

[ -n "${CUSTOM_OUT}" ] && DIST_DIR="${CUSTOM_OUT}"

log() { printf '[package-release] %s\n' "$*"; }
die() { printf '[package-release] ERROR: %s\n' "$*" >&2; exit 1; }

sha256sum_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}

# Verify required build artifacts exist.
for artifact in bzImage initramfs.cpio.gz; do
    [ -f "${OUT_DIR}/${artifact}" ] || \
        die "missing ${artifact} in ${OUT_DIR}; run scripts/build-image.sh first"
done

mkdir -p "${DIST_DIR}"

ARCHIVE_NAME="nuraos-${VERSION}"
ARCHIVE="${DIST_DIR}/${ARCHIVE_NAME}.tar.gz"
MANIFEST_COPY="${DIST_DIR}/${ARCHIVE_NAME}-manifest.json"
CHECKSUM="${ARCHIVE}.sha256"

log "packaging NuraOS ${VERSION} ..."

# Staging directory for the archive.
STAGING="$(mktemp -d)"
trap 'rm -rf "${STAGING}"' EXIT

mkdir -p "${STAGING}/${ARCHIVE_NAME}"
STAGE="${STAGING}/${ARCHIVE_NAME}"

# Mandatory artifacts.
cp "${OUT_DIR}/bzImage"             "${STAGE}/bzImage"
cp "${OUT_DIR}/initramfs.cpio.gz"   "${STAGE}/initramfs.cpio.gz"

# Optional /data image.
if [ -f "${OUT_DIR}/data.img" ]; then
    cp "${OUT_DIR}/data.img"        "${STAGE}/data.img"
    log "including data.img"
fi

# Manifest from build.
if [ -f "${OUT_DIR}/manifest.json" ]; then
    cp "${OUT_DIR}/manifest.json"   "${STAGE}/manifest.json"
    cp "${OUT_DIR}/manifest.json"   "${MANIFEST_COPY}"
fi

# Boot helper: copy run-qemu.sh and VERSIONS.env.
cp "${SCRIPT_DIR}/run-qemu.sh"      "${STAGE}/run-qemu.sh"
cp "${SCRIPT_DIR}/VERSIONS.env"     "${STAGE}/VERSIONS.env"

# Release notes placeholder.
cat > "${STAGE}/RELEASE.txt" <<RELEASE_EOF
NuraOS ${VERSION}
=================

Kernel: ${KERNEL_VERSION}
musl:   ${MUSL_VERSION}
BusyBox: ${BUSYBOX_VERSION}
Rust:   ${RUST_VERSION}
Go:     ${GO_VERSION}

Artifacts:
  bzImage            -- Linux kernel image
  initramfs.cpio.gz  -- Packed initramfs (nura-agent + gateway + BusyBox)
  data.img           -- Blank /data ext4 partition (128 MiB, if present)
  manifest.json      -- Build manifest with SHA-256 hashes

Boot with QEMU:
  ./run-qemu.sh

See https://github.com/YASSERRMD/NuraOS for full documentation.
RELEASE_EOF

# Pack the archive.
log "creating ${ARCHIVE} ..."
tar -czf "${ARCHIVE}" -C "${STAGING}" "${ARCHIVE_NAME}"

# Checksum.
HASH=$(sha256sum_file "${ARCHIVE}")
printf '%s  %s\n' "${HASH}" "$(basename "${ARCHIVE}")" > "${CHECKSUM}"

# Summary.
ARCHIVE_SIZE=$(stat -c %s "${ARCHIVE}" 2>/dev/null || stat -f %z "${ARCHIVE}" 2>/dev/null || echo 0)
log ""
log "Release archive: ${ARCHIVE}"
log "  size:    ${ARCHIVE_SIZE} bytes"
log "  sha256:  ${HASH}"
log "Checksum:  ${CHECKSUM}"
[ -f "${MANIFEST_COPY}" ] && log "Manifest:  ${MANIFEST_COPY}"
log ""
log "NuraOS ${VERSION} packaged successfully."
