#!/usr/bin/env bash
# Fetch musl libc at the pinned version, build it, and install a musl-gcc
# wrapper so later scripts can build fully-static x86-64 binaries.
#
# Output layout:
#   kernel/../third_party/musl-<version>/  (source)
#   kernel/../third_party/musl-install/    (headers, libs, musl-gcc wrapper)
#
# Usage: ./scripts/fetch-musl.sh [--force]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

. "${SCRIPT_DIR}/VERSIONS.env"

DOWNLOAD_DIR="${REPO_ROOT}/third_party/_download"
SOURCE_DIR="${REPO_ROOT}/third_party/musl-${MUSL_VERSION}"
INSTALL_DIR="${REPO_ROOT}/third_party/musl-install"
TARBALL="${DOWNLOAD_DIR}/${MUSL_TARBALL}"

FORCE=0
for arg in "$@"; do [ "${arg}" = "--force" ] && FORCE=1; done

log() { printf '[fetch-musl] %s\n' "$*"; }
die() { printf '[fetch-musl] ERROR: %s\n' "$*" >&2; exit 1; }

if [ -f "${INSTALL_DIR}/bin/musl-gcc" ] && [ "${FORCE}" -eq 0 ]; then
    log "musl-gcc already installed at ${INSTALL_DIR}/bin/musl-gcc; skipping (--force to redo)"
    exit 0
fi

for tool in curl tar sha256sum gcc make; do
    command -v "${tool}" >/dev/null 2>&1 || die "required tool not found: ${tool}"
done

mkdir -p "${DOWNLOAD_DIR}"

# Download.
if [ ! -f "${TARBALL}" ]; then
    log "downloading ${MUSL_TARBALL} ..."
    curl -L --progress-bar --fail -o "${TARBALL}" "${MUSL_URL}"
else
    log "tarball already downloaded: ${TARBALL}"
fi

# Record checksum.
SHA256=$(sha256sum "${TARBALL}" | awk '{print $1}')
log "SHA256: ${SHA256}"

# Extract.
log "extracting ..."
rm -rf "${SOURCE_DIR}"
tar -C "${REPO_ROOT}/third_party" -xzf "${TARBALL}"

# Build and install.
log "configuring musl (prefix=${INSTALL_DIR}) ..."
cd "${SOURCE_DIR}"
./configure \
    --prefix="${INSTALL_DIR}" \
    --target=x86_64-linux-musl \
    --disable-shared \
    --syslibdir="${INSTALL_DIR}/lib"

log "building musl ..."
make -j"$(nproc 2>/dev/null || echo 4)"

log "installing musl ..."
make install

log "musl installed to ${INSTALL_DIR}"
log "musl-gcc: ${INSTALL_DIR}/bin/musl-gcc"
log "done."
