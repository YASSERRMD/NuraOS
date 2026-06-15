#!/usr/bin/env bash
# Canonical static-compile entry point for NuraOS userland binaries.
# Wraps musl-gcc to enforce fully-static linking against musl libc.
#
# Usage: ./scripts/cc-musl.sh [gcc-flags...] source.c -o output
#
# All standard gcc flags are forwarded. The -static flag is always added.
#
# Examples:
#   ./scripts/cc-musl.sh hello.c -o hello
#   ./scripts/cc-musl.sh -O2 -Wall main.c utils.c -o agent-stub

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
INSTALL_DIR="${REPO_ROOT}/third_party/musl-install"
MUSL_GCC="${INSTALL_DIR}/bin/musl-gcc"

die() { printf '[cc-musl] ERROR: %s\n' "$*" >&2; exit 1; }

if [ ! -x "${MUSL_GCC}" ]; then
    die "musl-gcc not found at ${MUSL_GCC}; run scripts/fetch-musl.sh first"
fi

exec "${MUSL_GCC}" -static "$@"
