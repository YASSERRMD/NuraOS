#!/usr/bin/env bash
# Fetch BusyBox at the pinned version, apply the NuraOS config, build it
# statically against musl, and install the binary to the rootfs staging area.
#
# Prerequisites: scripts/fetch-musl.sh must have run first.
#
# Usage: ./scripts/fetch-busybox.sh [--force]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

. "${SCRIPT_DIR}/VERSIONS.env"

DOWNLOAD_DIR="${REPO_ROOT}/third_party/_download"
SOURCE_DIR="${REPO_ROOT}/third_party/busybox-${BUSYBOX_VERSION}"
INSTALL_DIR="${REPO_ROOT}/third_party/musl-install"
MUSL_GCC="${INSTALL_DIR}/bin/musl-gcc"
STAGING_BIN="${REPO_ROOT}/rootfs/staging/bin"
BB_CONFIG="${REPO_ROOT}/rootfs/busybox.config"
TARBALL="${DOWNLOAD_DIR}/${BUSYBOX_TARBALL}"

FORCE=0
for arg in "$@"; do [ "${arg}" = "--force" ] && FORCE=1; done

log() { printf '[fetch-busybox] %s\n' "$*"; }
die() { printf '[fetch-busybox] ERROR: %s\n' "$*" >&2; exit 1; }

[ -x "${MUSL_GCC}" ] || die "musl-gcc not found; run scripts/fetch-musl.sh first"

STAGING_BUSYBOX="${STAGING_BIN}/busybox"
if [ -f "${STAGING_BUSYBOX}" ] && [ "${FORCE}" -eq 0 ]; then
    log "busybox already at ${STAGING_BUSYBOX}; skipping (--force to rebuild)"
    exit 0
fi

for tool in curl tar bzip2 sha256sum make; do
    command -v "${tool}" >/dev/null 2>&1 || die "required tool not found: ${tool}"
done

mkdir -p "${DOWNLOAD_DIR}"

# Download.
if [ ! -f "${TARBALL}" ]; then
    log "downloading ${BUSYBOX_TARBALL} ..."
    curl -L --progress-bar --fail -o "${TARBALL}" "${BUSYBOX_URL}"
else
    log "tarball already downloaded: ${TARBALL}"
fi

SHA256=$(sha256sum "${TARBALL}" | awk '{print $1}')
log "SHA256: ${SHA256}"

# Extract.
log "extracting ..."
rm -rf "${SOURCE_DIR}"
tar -C "${REPO_ROOT}/third_party" -xjf "${TARBALL}"

# Copy config into place.
[ -f "${BB_CONFIG}" ] || die "BusyBox config not found: ${BB_CONFIG}"
cp "${BB_CONFIG}" "${SOURCE_DIR}/.config"

# Copy Linux kernel headers into musl's include tree (local-dev path only).
# BusyBox init and libbb need <linux/vt.h>, <linux/capability.h>, etc.
# musl-gcc uses -nostdinc so kernel headers must sit in the musl include tree.
# When musl was built from source via fetch-musl.sh the include tree is at
# INSTALL_DIR/include; copy there if it exists and linux/ is missing.
# In CI, musl comes from apt-installed musl-tools; the CI workflow copies
# kernel headers into /usr/include/x86_64-linux-musl via the
# "Set up musl toolchain" step (see .github/workflows/test.yml).
MUSL_INC="${INSTALL_DIR}/include"
if [ -d "${MUSL_INC}" ] && [ -d /usr/include/linux ] && [ ! -d "${MUSL_INC}/linux" ]; then
    log "copying Linux kernel headers into ${MUSL_INC} ..."
    cp -r /usr/include/linux       "${MUSL_INC}/linux"
    cp -r /usr/include/asm-generic "${MUSL_INC}/asm-generic" 2>/dev/null || true
    if [ -d /usr/include/x86_64-linux-gnu/asm ]; then
        cp -r /usr/include/x86_64-linux-gnu/asm "${MUSL_INC}/asm"
    elif [ -d /usr/include/asm ]; then
        cp -r /usr/include/asm "${MUSL_INC}/asm"
    fi
fi

# Build.
log "building busybox (static musl) ..."
# BusyBox's vendored kconfig does not expose 'olddefconfig'. Pipe yes "" into
# oldconfig so every new symbol gets its default value without manual input.
# pipefail is temporarily disabled because yes(1) exits with SIGPIPE (code 1)
# after make closes the pipe; that exit code must not propagate as a failure.
set +o pipefail
yes "" | make -C "${SOURCE_DIR}" \
    CC="${MUSL_GCC}" \
    HOSTCC=gcc \
    LDFLAGS="-static" \
    CONFIG_STATIC=y \
    oldconfig
set -o pipefail
# Force-disable MTD tools: NANDDUMP selects NANDWRITE via kconfig 'select',
# so it bypasses busybox.config=n and forces nandwrite.c to compile.
# nandwrite.c needs mtd/mtd-user.h which is not in the musl include tree.
# Patch the generated .config directly as belt-and-suspenders.
sed -i "s/^CONFIG_NANDDUMP=[ym]/CONFIG_NANDDUMP=n/;
        s/^CONFIG_NANDWRITE=[ym]/CONFIG_NANDWRITE=n/" "${SOURCE_DIR}/.config"
make -C "${SOURCE_DIR}" \
    CC="${MUSL_GCC}" \
    HOSTCC=gcc \
    LDFLAGS="-static" \
    CONFIG_STATIC=y \
    -j"$(nproc 2>/dev/null || echo 4)"

BB_BIN="${SOURCE_DIR}/busybox"
[ -f "${BB_BIN}" ] || die "busybox binary not produced"

# Verify static.
if file "${BB_BIN}" | grep -q "statically linked"; then
    log "OK: busybox is statically linked"
elif ldd "${BB_BIN}" 2>&1 | grep -q "not a dynamic executable"; then
    log "OK: busybox is not a dynamic executable"
else
    log "WARNING: static check inconclusive: $(file "${BB_BIN}")"
fi

SIZE=$(du -h "${BB_BIN}" | cut -f1)
log "busybox binary: ${BB_BIN} (${SIZE})"

# Install to staging.
mkdir -p "${STAGING_BIN}"
cp "${BB_BIN}" "${STAGING_BUSYBOX}"
chmod 755 "${STAGING_BUSYBOX}"

log "installed to ${STAGING_BUSYBOX}"
log "done. Run scripts/build-initramfs.sh to assemble the initramfs."
