#!/usr/bin/env bash
# Fetch the Linux kernel tarball at the pinned version, verify the PGP
# signature, and extract it to kernel/linux.
#
# Usage: ./scripts/fetch-kernel.sh [--force]
#
# The script is idempotent: if kernel/linux already exists at the correct
# version it exits early unless --force is given.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

. "${SCRIPT_DIR}/VERSIONS.env"

KERNEL_DIR="${REPO_ROOT}/kernel"
LINUX_DIR="${KERNEL_DIR}/linux"
DOWNLOAD_DIR="${KERNEL_DIR}/_download"
TARBALL="${DOWNLOAD_DIR}/${KERNEL_TARBALL}"
SIGN_FILE="${DOWNLOAD_DIR}/${KERNEL_TARBALL%.xz}.sign"

FORCE=0
for arg in "$@"; do
    [ "${arg}" = "--force" ] && FORCE=1
done

log() { printf '[fetch-kernel] %s\n' "$*"; }
die() { printf '[fetch-kernel] ERROR: %s\n' "$*" >&2; exit 1; }

# Check if the kernel is already present at the right version.
if [ -d "${LINUX_DIR}" ] && [ "${FORCE}" -eq 0 ]; then
    if [ -f "${LINUX_DIR}/Makefile" ]; then
        existing=$(grep -m1 '^VERSION\s*=' "${LINUX_DIR}/Makefile" | tr -d ' ' | cut -d= -f2).$(grep -m1 '^PATCHLEVEL\s*=' "${LINUX_DIR}/Makefile" | tr -d ' ' | cut -d= -f2).$(grep -m1 '^SUBLEVEL\s*=' "${LINUX_DIR}/Makefile" | tr -d ' ' | cut -d= -f2) 2>/dev/null || existing="unknown"
        if [ "${existing}" = "${KERNEL_VERSION}" ]; then
            log "kernel/linux is already at ${KERNEL_VERSION}; skipping (use --force to re-fetch)"
            exit 0
        fi
        log "kernel/linux is at ${existing}, want ${KERNEL_VERSION}; re-fetching"
    fi
fi

# Ensure required tools.
for tool in curl xz tar gpg sha256sum; do
    command -v "${tool}" >/dev/null 2>&1 || die "required tool not found: ${tool}"
done

mkdir -p "${DOWNLOAD_DIR}"

# Download tarball.
if [ ! -f "${TARBALL}" ]; then
    log "downloading ${KERNEL_TARBALL} ..."
    curl -L --progress-bar --fail \
        -o "${TARBALL}" \
        "${KERNEL_URL}"
else
    log "tarball already downloaded: ${TARBALL}"
fi

# Download detached signature (used for PGP verification via xz decompressor).
if [ ! -f "${SIGN_FILE}" ]; then
    log "downloading signature ..."
    curl -L --progress-bar --fail \
        -o "${SIGN_FILE}" \
        "${KERNEL_SIG_URL}"
fi

# Verify signature using the Linux kernel developer key ring.
# The kernel.org tarball is GPG-signed. We verify if gpg keys are available;
# we record the outcome either way and fail the build only on a clear bad sig.
log "verifying PGP signature ..."
GPG_RESULT="SKIPPED"
if gpg --list-keys torvalds@kernel.org >/dev/null 2>&1 || \
   gpg --list-keys gregkh@kernel.org   >/dev/null 2>&1; then
    # Decompress and pipe to gpg for verification.
    if xz -cd "${TARBALL}" | gpg --verify "${SIGN_FILE}" - 2>&1; then
        GPG_RESULT="PASS"
        log "PGP signature: PASS"
    else
        GPG_RESULT="FAIL"
        die "PGP signature verification FAILED for ${KERNEL_TARBALL}"
    fi
else
    log "WARNING: kernel.org GPG keys not in keyring; skipping PGP verification."
    log "To enable: gpg --locate-keys torvalds@kernel.org gregkh@kernel.org"
    GPG_RESULT="SKIPPED_NO_KEY"
fi

# Record SHA256 of the downloaded tarball.
SHA256=$(sha256sum "${TARBALL}" | awk '{print $1}')
log "SHA256: ${SHA256}"

# Extract kernel tree.
log "extracting ${KERNEL_TARBALL} to ${LINUX_DIR} ..."
rm -rf "${LINUX_DIR}"
tar -C "${KERNEL_DIR}" -xf "${TARBALL}"
# The tarball extracts to linux-X.Y.Z/; rename it.
mv "${KERNEL_DIR}/linux-${KERNEL_VERSION}" "${LINUX_DIR}"

log "kernel source at: ${LINUX_DIR}"
log "version: ${KERNEL_VERSION}"

# Write pin record.
PINNED_FILE="${KERNEL_DIR}/PINNED.md"
cat > "${PINNED_FILE}" <<EOF
# Kernel Pin Record

| Field        | Value                        |
|--------------|------------------------------|
| Tag          | ${KERNEL_TAG}                |
| Version      | ${KERNEL_VERSION}            |
| Tarball      | ${KERNEL_TARBALL}            |
| URL          | ${KERNEL_URL}                |
| SHA256       | ${SHA256}                    |
| GPG verify   | ${GPG_RESULT}                |
| Fetched      | $(date -u '+%Y-%m-%dT%H:%M:%SZ') |

## Signature file

${KERNEL_SIG_URL}

## Notes

To re-verify manually after fetch:
  xz -cd kernel/_download/${KERNEL_TARBALL} | gpg --verify kernel/_download/${KERNEL_TARBALL%.xz}.sign -

To re-fetch from scratch:
  ./scripts/fetch-kernel.sh --force
EOF

log "pin record written to ${PINNED_FILE}"
log "done."
