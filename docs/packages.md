# NuraOS Package Format

NuraOS uses a signed package format (`.nupkg`) for optional add-ons. Packages
are gzip-compressed tar archives carrying a manifest, an Ed25519 signature, and
payload files. The installer verifies signatures and SHA-256 checksums before
writing any files to disk.

## Archive layout

```
my-addon-1.0.0.nupkg  (gzip-compressed tar)
  manifest.json        JSON manifest (name, version, files, checksums, hooks)
  manifest.sig         Raw 64-byte Ed25519 signature of manifest.json content
  sbin/my-tool         Payload files (relative paths under the overlay root)
  etc/my-addon.toml
  ...
```

## Manifest schema

```json
{
  "schema": 1,
  "name": "my-addon",
  "version": "1.2.3",
  "description": "Optional add-on for NuraOS",
  "arch": "x86_64",
  "depends": ["base"],
  "files": [
    {
      "path": "sbin/my-tool",
      "sha256": "abc123...",
      "mode": "0755"
    },
    {
      "path": "etc/my-addon.toml",
      "sha256": "def456...",
      "mode": "0644"
    }
  ],
  "install_hook": "sbin/my-tool --install",
  "remove_hook":  "sbin/my-tool --remove"
}
```

| Field | Description |
|---|---|
| `schema` | Must be `1` |
| `name` | Unique package name (alphanumeric and `-`) |
| `version` | Semantic version string |
| `arch` | Target architecture (`x86_64`, `aarch64`) |
| `depends` | Names of packages that must already be installed |
| `files` | List of files with relative paths, SHA-256 digests, and octal modes |
| `install_hook` | Optional command run after installation (relative to overlay) |
| `remove_hook` | Optional command run before removal (relative to overlay) |

## Signature

The signature in `manifest.sig` is a raw 64-byte Ed25519 signature of the exact
bytes of `manifest.json` in the archive. Only the manifest is signed; the
payload files are integrity-protected by the SHA-256 checksums *inside* the
signed manifest.

The installer rejects any package where:
- `manifest.sig` is absent or not exactly 64 bytes
- The Ed25519 signature does not verify against the trusted public key
- Any payload file's SHA-256 does not match the manifest

No partial installation occurs on verification failure.

## Installation paths

Files are extracted to `/data/overlay/` preserving relative paths:

```
/data/overlay/sbin/my-tool
/data/overlay/etc/my-addon.toml
```

The package database lives at `/data/packages/db.json`.

To use installed binaries, add `/data/overlay/sbin` to `PATH`:

```sh
export PATH="/data/overlay/sbin:${PATH}"
```

## Key management

### Generate a keypair

```sh
./scripts/pkg-keygen.sh /secure/location
# Produces:
#   /secure/location/pkg.priv  (hex Ed25519 private key; keep offline)
#   /secure/location/pkg.pub   (hex Ed25519 public key; ship in initramfs)
```

Install the public key in the initramfs:

```sh
cp /secure/location/pkg.pub rootfs/etc/nura/pkg.pub
# Rebuild the initramfs to include it.
```

### Override the public key path at runtime

```sh
export NURA_PKG_PUBKEY=/data/etc/custom-pkg.pub
```

## nuractl commands

```sh
# Install a package (verifies signature + checksums).
nuractl pkg install my-addon-1.2.3.nupkg

# List installed packages.
nuractl pkg list

# Remove a package (runs remove hook; refuses if dependents exist).
nuractl pkg remove my-addon

# JSON output.
nuractl --json pkg list
```

Environment overrides:

| Variable | Default | Purpose |
|---|---|---|
| `NURA_PKG_PUBKEY` | `/etc/nura/pkg.pub` | Path to the Ed25519 public key |
| `NURA_PKG_DB` | `/data/packages/db.json` | Package database path |
| `NURA_PKG_OVERLAY` | `/data/overlay` | Overlay root for extracted files |

## Security model

- **Unsigned packages are always rejected.** There is no `--force` or bypass flag.
- **Tampered payloads are detected.** SHA-256 is recomputed on every file after
  extraction; mismatches cause the install to fail before any hook runs.
- **Dependencies are checked at install and remove time.** Installing a package
  whose dependency is absent fails; removing a package that others depend on fails.
- **Hooks are run with no elevated privileges.** They execute as the invoking user
  inside the overlay directory.
- The package format is intentionally simple. It does not support pre/post-install
  scripts with elevated privileges, kernel module installation, or arbitrary root
  access. Packages are add-ons to userland only.
