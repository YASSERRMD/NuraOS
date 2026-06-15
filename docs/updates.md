# Update Transactions

NuraOS uses transactional A/B rootfs updates. The inactive slot receives the
new image; the system switches to it atomically on commit. If anything fails
before the commit, the running slot is untouched. Interrupted updates are
detected on the next boot and cleaned up.

## Architecture

```
/data/etc/active-slot      -- 'a' or 'b'; read by the QEMU boot script
/data/update/tx.json       -- current or last transaction record
/data/update/staging/<id>/ -- staged image before commit (deleted after commit)
/data/update/audit.log     -- append-only audit log (separate from journal)
/boot/rootfs-a.ext4        -- slot A rootfs image
/boot/rootfs-b.ext4        -- slot B rootfs image
```

## Transaction lifecycle

```
BEGIN
  stage image to /data/update/staging/<id>/rootfs-<inactive>.ext4
  compute SHA-256 while writing
VERIFY
  check SHA-256 against expected value (if provided)
  check Ed25519 signature over SHA-256 (if key configured)
  snapshot health state (active slot + running services)
COMMIT (atomic)
  rename staged file into /boot/rootfs-<inactive>.ext4
  write /data/etc/active-slot = <inactive>
  record committed state in tx.json
DONE
  delete staging directory
  append 'tx.committed' to audit.log
```

On any error the staged file is deleted, tx.json is set to `"aborted"`, and the
active slot is unchanged.

## Interrupted update recovery

If power is lost between staging and commit, tx.json will have state `"staging"`
or `"verifying"` on the next boot. `nura-manager` detects this automatically and:

1. Logs a warning: `interrupted update transaction detected and aborted`.
2. Deletes the staging directory.
3. Sets tx.json state to `"aborted"`.
4. Continues normal startup on the unchanged slot.

No manual intervention is required after an interrupted update.

## Ed25519 signature

Images are signed with the same Ed25519 key pair used for packages (see
[packages.md](packages.md)). The signer signs the hex-encoded SHA-256 of the
image:

```sh
# Sign an image (on the build machine with the private key)
sha256=$(sha256sum rootfs.ext4 | cut -d' ' -f1)
printf '%s' "${sha256}" | openssl pkeyutl -sign -inkey pkg.priv -out rootfs.ext4.sig
```

Or with Go:

```go
sig := ed25519.Sign(privKey, []byte(sha256Hex))
```

Configure the public key path with `NURA_PKG_PUBKEY` (default `/etc/nura/pkg.pub`).

## nuractl commands

```sh
# Apply a local image to the inactive slot (no sig verification).
nuractl update apply /path/to/rootfs-new.ext4

# Apply with SHA-256 check.
nuractl update apply /path/to/rootfs-new.ext4 --sha256 <hex>

# Rollback: revert active-slot to the pre-update slot.
nuractl update rollback

# Abort an in-progress transaction (deletes staging files).
nuractl update abort

# View the update audit log.
nuractl update log
nuractl --json update log
```

Environment overrides:

| Variable | Default | Purpose |
|---|---|---|
| `NURA_DATA_DIR` | `/data` | Data partition root |
| `NURA_ROOTFS_DIR` | `/boot` | Directory with rootfs-{a,b}.ext4 |
| `NURA_PKG_PUBKEY` | `/etc/nura/pkg.pub` | Ed25519 public key for image verification |

## Pre-update health snapshot

Before committing, the transaction records:

```json
{
  "health_snapshot": {
    "active_slot": "a",
    "timestamp": "2026-01-01T00:00:00Z",
    "services_running": ["gateway", "nura-agent", "llama-server"]
  }
}
```

This snapshot is used by `rollback` to determine which slot to revert to.

## Audit log

`/data/update/audit.log` is append-only, line-delimited JSON:

```json
{"ts":"2026-01-01T00:00:00Z","tx_id":"1a2b3c","event":"tx.begin","detail":"staging to inactive slot b from /path/img"}
{"ts":"2026-01-01T00:00:01Z","tx_id":"1a2b3c","event":"tx.staged","detail":"sha256=abc... size_ok"}
{"ts":"2026-01-01T00:00:01Z","tx_id":"1a2b3c","event":"tx.sig_ok","detail":"ed25519 signature verified"}
{"ts":"2026-01-01T00:00:01Z","tx_id":"1a2b3c","event":"tx.committed","detail":"active slot now b"}
```

The log is separate from the journal so update history survives journal rotation.

## Integration with shell scripts

The existing `scripts/update.sh` and `scripts/slot-select.sh` remain available
for direct slot manipulation. For transactional updates with verification, use
`nuractl update apply` which:
- Stages atomically
- Verifies before committing
- Handles interruption recovery
- Writes the audit log

The shell scripts are appropriate for emergency manual slot switches or
development workflows where verification is not required.
