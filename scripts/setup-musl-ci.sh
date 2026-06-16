#!/usr/bin/env bash
# Expose the apt-installed musl-gcc at the path fetch-busybox.sh expects and make
# Linux kernel headers visible to it. Used by the CI workflows (test.yml,
# smoke.yml, release.yml) in place of building musl from source via fetch-musl.sh.
#
# Why this is needed:
#   * fetch-busybox.sh looks for musl-gcc at third_party/musl-install/bin/musl-gcc
#     (the fetch-musl.sh output path). In CI musl comes from the apt `musl-tools`
#     package, which installs musl-gcc on PATH instead -- so we symlink it into
#     that expected location.
#   * musl-gcc compiles with -nostdinc, so the kernel UAPI headers BusyBox needs
#     (<linux/*.h>, <asm/*.h>) must sit inside musl's own include tree. The
#     system musl include dir (/usr/include/x86_64-linux-musl) is not
#     world-writable, hence sudo. Kernel headers contain no libc symbols and
#     cannot shadow musl's C library headers.
#
# Idempotent: safe to run more than once. Requires `musl-tools` already installed.
#
# Usage: ./scripts/setup-musl-ci.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

log() { printf '[setup-musl-ci] %s\n' "$*"; }
die() { printf '[setup-musl-ci] ERROR: %s\n' "$*" >&2; exit 1; }

MUSL_GCC="$(command -v musl-gcc || true)"
[ -n "${MUSL_GCC}" ] || die "musl-gcc not on PATH; install the musl-tools package first"

# Expose musl-gcc at the path fetch-busybox.sh expects.
mkdir -p "${REPO_ROOT}/third_party/musl-install/bin"
ln -sf "${MUSL_GCC}" "${REPO_ROOT}/third_party/musl-install/bin/musl-gcc"
log "linked third_party/musl-install/bin/musl-gcc -> ${MUSL_GCC}"

# Copy Linux kernel headers into musl's system include tree if absent.
MUSL_INC=/usr/include/x86_64-linux-musl
if [ -d "${MUSL_INC}" ] && [ ! -d "${MUSL_INC}/linux" ]; then
    log "copying Linux kernel headers into ${MUSL_INC} ..."
    sudo cp -r /usr/include/linux       "${MUSL_INC}/linux"
    sudo cp -r /usr/include/asm-generic "${MUSL_INC}/asm-generic" || true
    if [ -d /usr/include/x86_64-linux-gnu/asm ]; then
        sudo cp -r /usr/include/x86_64-linux-gnu/asm "${MUSL_INC}/asm"
    elif [ -d /usr/include/asm ]; then
        sudo cp -r /usr/include/asm "${MUSL_INC}/asm"
    fi
fi

log "done."
