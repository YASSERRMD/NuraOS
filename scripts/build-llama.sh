#!/usr/bin/env bash
# Build llama-server (CPU-only, fully static) against musl and install into
# rootfs/staging/sbin/llama-server.
#
# Prerequisites: cmake >= 3.14, musl-gcc wrapper (scripts/fetch-musl.sh),
# llama.cpp source (scripts/fetch-llama.sh).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "${REPO_ROOT}/scripts/VERSIONS.env"

SRC="${REPO_ROOT}/third_party/llama-cpp"
BUILD_DIR="${REPO_ROOT}/third_party/llama-build"
STAGING="${REPO_ROOT}/rootfs/staging"
MUSL_INSTALL="${REPO_ROOT}/third_party/musl-install"
MUSL_GCC="${MUSL_INSTALL}/bin/musl-gcc"
NPROC="${NPROC:-$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)}"

if [ ! -d "${SRC}/.git" ]; then
    echo "ERROR: llama.cpp source not found at ${SRC}"
    echo "Run: bash scripts/fetch-llama.sh"
    exit 1
fi

if [ ! -x "${MUSL_GCC}" ]; then
    echo "ERROR: musl-gcc not found at ${MUSL_GCC}"
    echo "Run: bash scripts/fetch-musl.sh"
    exit 1
fi

mkdir -p "${BUILD_DIR}" "${STAGING}/sbin"

echo "Configuring llama.cpp (CPU-only, static musl) ..."
cmake -S "${SRC}" -B "${BUILD_DIR}" \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_C_COMPILER="${MUSL_GCC}" \
    -DCMAKE_CXX_COMPILER="${MUSL_GCC}" \
    -DCMAKE_EXE_LINKER_FLAGS="-static" \
    -DCMAKE_SHARED_LIBRARY_LINK_C_FLAGS="" \
    -DCMAKE_SHARED_LIBRARY_LINK_CXX_FLAGS="" \
    -DBUILD_SHARED_LIBS=OFF \
    -DLLAMA_BUILD_SERVER=ON \
    -DLLAMA_BUILD_TESTS=OFF \
    -DLLAMA_BUILD_EXAMPLES=OFF \
    -DGGML_NATIVE=OFF \
    -DGGML_AVX=ON \
    -DGGML_AVX2=OFF \
    -DGGML_F16C=OFF \
    -DGGML_FMA=OFF

echo "Building llama-server with ${NPROC} jobs ..."
cmake --build "${BUILD_DIR}" --target llama-server -j"${NPROC}"

SERVER_BIN="${BUILD_DIR}/bin/llama-server"
if [ ! -f "${SERVER_BIN}" ]; then
    SERVER_BIN="$(find "${BUILD_DIR}" -name 'llama-server' -type f | head -1)"
fi

if [ -z "${SERVER_BIN}" ] || [ ! -f "${SERVER_BIN}" ]; then
    echo "ERROR: llama-server binary not found after build"
    exit 1
fi

cp "${SERVER_BIN}" "${STAGING}/sbin/llama-server"
chmod 755 "${STAGING}/sbin/llama-server"

SIZE="$(wc -c < "${STAGING}/sbin/llama-server")"
echo "llama-server installed: ${STAGING}/sbin/llama-server (${SIZE} bytes)"

file "${STAGING}/sbin/llama-server" || true
