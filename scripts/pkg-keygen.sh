#!/usr/bin/env bash
# Generate an Ed25519 keypair for signing NuraOS packages (.nupkg).
#
# Output:
#   pkg.priv   -- hex-encoded 64-byte Ed25519 private key (keep secret)
#   pkg.pub    -- hex-encoded 32-byte Ed25519 public key
#
# The public key should be installed into the initramfs at:
#   /etc/nura/pkg.pub
#
# Usage:
#   ./scripts/pkg-keygen.sh [output-dir]
#
# The output directory defaults to the current directory.

set -euo pipefail

OUT_DIR="${1:-${PWD}}"
PRIV="${OUT_DIR}/pkg.priv"
PUB="${OUT_DIR}/pkg.pub"

if [ -f "${PRIV}" ] || [ -f "${PUB}" ]; then
    echo "ERROR: key files already exist (${PRIV}, ${PUB})" >&2
    echo "Delete them first if you want to generate a new keypair." >&2
    exit 1
fi

# Use Go to generate the keypair (avoids openssl dependency).
go run - "${PRIV}" "${PUB}" <<'GOEOF'
package main

import (
    "crypto/ed25519"
    "crypto/rand"
    "encoding/hex"
    "fmt"
    "os"
)

func main() {
    priv_path := os.Args[1]
    pub_path  := os.Args[2]

    pub, priv, err := ed25519.GenerateKey(rand.Reader)
    if err != nil {
        fmt.Fprintf(os.Stderr, "key generation failed: %v\n", err)
        os.Exit(1)
    }

    privHex := hex.EncodeToString(priv)
    pubHex  := hex.EncodeToString(pub)

    if err := os.WriteFile(priv_path, []byte(privHex+"\n"), 0o600); err != nil {
        fmt.Fprintf(os.Stderr, "write private key: %v\n", err)
        os.Exit(1)
    }
    if err := os.WriteFile(pub_path, []byte(pubHex+"\n"), 0o644); err != nil {
        fmt.Fprintf(os.Stderr, "write public key: %v\n", err)
        os.Exit(1)
    }

    fmt.Printf("keypair generated\n")
    fmt.Printf("  private key: %s  (keep secret; sign packages with nura-pkg-sign)\n", priv_path)
    fmt.Printf("  public key:  %s  (install at /etc/nura/pkg.pub in initramfs)\n", pub_path)
}
GOEOF
