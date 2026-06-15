#!/usr/bin/env bash
# Fetch llama.cpp source at the pinned SHA into third_party/llama-cpp.
# Runs on the build host (Linux x86_64 preferred; macOS works for inspection).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "${REPO_ROOT}/scripts/VERSIONS.env"

DEST="${REPO_ROOT}/third_party/llama-cpp"
DOWNLOAD_DIR="${REPO_ROOT}/third_party/_download"

mkdir -p "${DOWNLOAD_DIR}"

if [ -d "${DEST}/.git" ]; then
    current_sha="$(git -C "${DEST}" rev-parse --short HEAD)"
    if [ "${current_sha}" = "${LLAMA_SHA}" ]; then
        echo "llama.cpp already at ${LLAMA_SHA} -- nothing to do"
        exit 0
    fi
    echo "llama.cpp present at ${current_sha}, re-fetching to ${LLAMA_SHA}"
    rm -rf "${DEST}"
fi

echo "Fetching llama.cpp at SHA ${LLAMA_SHA} ..."
git clone --filter=blob:none --no-checkout "${LLAMA_REPO}" "${DEST}"
git -C "${DEST}" checkout "${LLAMA_SHA}"

FULL_SHA="$(git -C "${DEST}" rev-parse HEAD)"

cat > "${DEST}/PINNED.md" <<EOF
# llama.cpp vendor pin

| Field   | Value                              |
|---------|------------------------------------|
| Repo    | ${LLAMA_REPO}                      |
| Short   | ${LLAMA_SHA}                       |
| Full    | ${FULL_SHA}                        |
| Purpose | CPU inference server for NuraOS    |
EOF

echo "llama.cpp fetched: ${FULL_SHA}"
echo "Source: ${DEST}"
