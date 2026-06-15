#!/usr/bin/env bash
# Generate and sign a NuraOS boot integrity manifest.
#
# Computes SHA-256 of each listed file, builds boot-manifest.json, signs it
# with an Ed25519 private key, and writes a sha256sum-compatible boot-hashes
# file. All three files are written to OUT_DIR.
#
# Usage:
#   ./scripts/sign-rootfs.sh --key PRIVKEY_HEX --slot a|b FILE [FILE ...]
#
# Options:
#   --key PATH     path to Ed25519 private key (hex, 64 bytes = 128 hex chars)
#   --pub-key PATH path to Ed25519 public key  (hex, 32 bytes = 64 hex chars)
#   --slot a|b     active A/B slot (default: a)
#   --out DIR      output directory (default: image/out/data-root/etc)
#   --verify       verify an existing manifest instead of generating
#
# Key generation (if you don't have a key yet):
#   Run: go run ./cmd/nuractl pkg -- does not apply; instead use pkg-keygen.sh
#   or generate with openssl:
#     openssl genpkey -algorithm ED25519 -out boot.pem
#     # Extract raw private key hex:
#     openssl pkey -in boot.pem -outform DER | xxd -p -c 256 | tail -c 129 | head -c 128
#
# Example:
#   ./scripts/sign-rootfs.sh --key "$(cat boot.key.hex)" --slot a \
#       image/out/bzImage image/out/initramfs.cpio.gz

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUT_DIR="${REPO_ROOT}/image/out/data-root/etc"
SLOT="a"
KEY_PATH=""
PUB_KEY_PATH=""
VERIFY=0
FILES=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --key)     shift; KEY_PATH="$1" ;;
        --pub-key) shift; PUB_KEY_PATH="$1" ;;
        --slot)    shift; SLOT="$1" ;;
        --out)     shift; OUT_DIR="$1" ;;
        --verify)  VERIFY=1 ;;
        -*)        printf '[sign-rootfs] unknown option: %s\n' "$1" >&2; exit 1 ;;
        *)         FILES+=("$1") ;;
    esac
    shift
done

log() { printf '[sign-rootfs] %s\n' "$*"; }
die() { printf '[sign-rootfs] ERROR: %s\n' "$*" >&2; exit 1; }

mkdir -p "${OUT_DIR}"

MANIFEST_PATH="${OUT_DIR}/boot-manifest.json"
SIG_PATH="${OUT_DIR}/boot-manifest.sig"
HASHES_PATH="${OUT_DIR}/boot-hashes"

if [ "${VERIFY}" -eq 1 ]; then
    [ -n "${PUB_KEY_PATH}" ] || die "--pub-key required for --verify"
    [ -f "${MANIFEST_PATH}" ] || die "no manifest at ${MANIFEST_PATH}"
    [ -f "${SIG_PATH}" ]      || die "no signature at ${SIG_PATH}"
    log "verifying manifest ${MANIFEST_PATH} ..."
    # Use nuractl if available.
    if command -v nuractl >/dev/null 2>&1; then
        nuractl integrity verify \
            --pub-key "${PUB_KEY_PATH}" \
            "${MANIFEST_PATH}" "${SIG_PATH}"
    else
        die "nuractl not found; build it first with scripts/build-nuractl.sh"
    fi
    log "verification OK"
    exit 0
fi

[ "${#FILES[@]}" -gt 0 ] || die "no files specified"
[ -n "${KEY_PATH}" ] || die "--key required"

log "hashing ${#FILES[@]} file(s) ..."
for f in "${FILES[@]}"; do
    [ -f "$f" ] || die "file not found: $f"
done

# Build the manifest JSON and sign it via a small Go helper embedded here.
# We use 'go run' with a heredoc program rather than a separate file.
TMPDIR_GO=$(mktemp -d)
trap "rm -rf ${TMPDIR_GO}" EXIT

cat > "${TMPDIR_GO}/main.go" << 'GOEOF'
package main

import (
    "crypto/ed25519"
    "encoding/hex"
    "encoding/json"
    "encoding/base64"
    "crypto/sha256"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "time"
)

type Entry struct {
    Path   string `json:"path"`
    SHA256 string `json:"sha256"`
}
type Manifest struct {
    Schema    int     `json:"schema"`
    Timestamp string  `json:"timestamp"`
    Slot      string  `json:"slot,omitempty"`
    Entries   []Entry `json:"entries"`
}

func hashFile(path string) (string, error) {
    f, err := os.Open(path)
    if err != nil { return "", err }
    defer f.Close()
    h := sha256.New()
    if _, err := io.Copy(h, f); err != nil { return "", err }
    return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func main() {
    if len(os.Args) < 5 {
        fmt.Fprintln(os.Stderr, "usage: prog <keyHex> <slot> <outDir> file [file ...]")
        os.Exit(1)
    }
    keyHex := os.Args[1]
    slot := os.Args[2]
    outDir := os.Args[3]
    files := os.Args[4:]

    rawKey, err := hex.DecodeString(keyHex)
    if err != nil || len(rawKey) != ed25519.PrivateKeySize {
        fmt.Fprintln(os.Stderr, "invalid key: must be 128 hex chars (64-byte Ed25519 private key)")
        os.Exit(1)
    }
    priv := ed25519.PrivateKey(rawKey)

    m := Manifest{Schema: 1, Timestamp: time.Now().UTC().Format(time.RFC3339), Slot: slot}
    for _, p := range files {
        sum, err := hashFile(p)
        if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
        m.Entries = append(m.Entries, Entry{Path: p, SHA256: sum})
    }

    manifestData, _ := json.MarshalIndent(m, "", "  ")
    sig := ed25519.Sign(priv, manifestData)

    os.MkdirAll(outDir, 0o755)
    os.WriteFile(filepath.Join(outDir, "boot-manifest.json"), append(manifestData, '\n'), 0o644)
    os.WriteFile(filepath.Join(outDir, "boot-manifest.sig"),
        []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644)

    hf, _ := os.Create(filepath.Join(outDir, "boot-hashes"))
    defer hf.Close()
    for _, e := range m.Entries {
        fmt.Fprintf(hf, "%s  %s\n", e.SHA256, e.Path)
    }

    fmt.Printf("manifest: %s\n", filepath.Join(outDir, "boot-manifest.json"))
    fmt.Printf("sig:      %s\n", filepath.Join(outDir, "boot-manifest.sig"))
    fmt.Printf("hashes:   %s\n", filepath.Join(outDir, "boot-hashes"))
    fmt.Printf("entries:  %d\n", len(m.Entries))
}
GOEOF

log "generating manifest and signature ..."
(cd "${TMPDIR_GO}" && go run main.go "${KEY_PATH}" "${SLOT}" "${OUT_DIR}" "${FILES[@]}")

log "done"
log "  manifest: ${MANIFEST_PATH}"
log "  sig:      ${SIG_PATH}"
log "  hashes:   ${HASHES_PATH}"
log ""
log "Deploy ${HASHES_PATH} and ${MANIFEST_PATH} to /data/etc/ on the target."
log "The hashes file is checked by rootfs/init at every boot."
