# Suite T09 - provenance-security

Verifies that NuraOS build artifacts are accompanied by a valid provenance
manifest, that secrets are not exposed on the console during boot, and that
the running system reports integrity status.

## Cases

| Case | Source | Pass condition |
|------|--------|----------------|
| `manifest-exists` | `image/out/manifest.json` | File present and parses as JSON |
| `manifest-has-hashes` | `image/out/manifest.json` | Contains at least one `sha256` field |
| `no-secrets-in-image` | Serial log | No API key patterns (`sk-ant-api`, `sk-proj-`, `Bearer sk-`) in boot log |
| `integrity-status` | GET /status | Response body mentions `integrity`; **skip** if not yet implemented |
| `version-pinned` | `image/out/manifest.json` | `nura_version` and `kernel_version` fields present |

## Prerequisites

- `image/out/manifest.json` produced by the build pipeline.
- A booted NuraOS QEMU instance (`SerialLogPath` available).

## Running

```
go run ./cmd/run-suite -- provenance-security
```
