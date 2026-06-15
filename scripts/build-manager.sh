#!/bin/sh
# Build the nura-manager binary as a fully static Linux/amd64 binary.
#
# Output: rootfs/staging/sbin/nura-manager
# Requires: Go (version from scripts/VERSIONS.env)
set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

. "${SCRIPT_DIR}/VERSIONS.env"

SERVICES_DIR="${REPO_ROOT}/services"
OUTPUT="${REPO_ROOT}/rootfs/staging/sbin/nura-manager"

BUILD_VERSION="${NURA_VERSION:-$(git -C "${REPO_ROOT}" describe --tags --always 2>/dev/null || echo 'dev')}"

echo "==> Building nura-manager ${BUILD_VERSION} ..."

mkdir -p "$(dirname "${OUTPUT}")"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-s -w" \
    -o "${OUTPUT}" \
    "${SERVICES_DIR}/cmd/nura-manager"

echo "==> Installed: ${OUTPUT}"
file "${OUTPUT}" 2>/dev/null || true
