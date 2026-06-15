#!/usr/bin/env bash
# Build the hello-world musl smoke test and verify it is fully static.
#
# Usage: ./rootfs/tests/build-hello.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
CC_MUSL="${REPO_ROOT}/scripts/cc-musl.sh"
SRC="${SCRIPT_DIR}/hello.c"
OUT="${SCRIPT_DIR}/hello"

log() { printf '[build-hello] %s\n' "$*"; }
die() { printf '[build-hello] ERROR: %s\n' "$*" >&2; exit 1; }

[ -x "${CC_MUSL}" ] || die "cc-musl.sh not found; is the repo root at ${REPO_ROOT}?"
[ -f "${SRC}" ]     || die "source not found: ${SRC}"

log "compiling hello.c ..."
"${CC_MUSL}" -O2 -Wall "${SRC}" -o "${OUT}"

log "verifying static linkage ..."
if file "${OUT}" | grep -q "statically linked"; then
    log "OK: binary is statically linked"
elif ldd "${OUT}" 2>&1 | grep -q "not a dynamic executable"; then
    log "OK: binary is not a dynamic executable"
else
    die "binary does not appear to be fully static: $(file "${OUT}")"
fi

SIZE=$(du -h "${OUT}" | cut -f1)
log "binary: ${OUT} (${SIZE})"

log "running smoke test ..."
OUTPUT=$("${OUT}")
[ "${OUTPUT}" = "hello from NuraOS musl static build" ] || \
    die "unexpected output: ${OUTPUT}"
log "output: ${OUTPUT}"
log "smoke test PASSED."
