# NuraOS Compliance and Data Residency

This document describes what data NuraOS stores, where it lives, how long it
is retained, and how data-residency policy is enforced for AI provider routing.

---

## Data inventory

| Data type | Location | Retention default | Can leave device? |
|-----------|----------|-------------------|--------------------|
| Session transcripts | `/data/sessions/` | 90 days | No (local only by default) |
| Journal entries | `/data/journal/` | 90 days | No |
| Provenance records | `/data/provenance/` | 90 days | No |
| Crash captures | `/data/crashes/` | 20 files (rotated) | No (redacted) |
| Config snapshot | `/data/config/nura.json` | Indefinite | No |
| Config history | `/data/config/history.jsonl` | 50 entries | No |
| Model files | `/data/models/` | Indefinite | No |
| Machine identity | `/data/identity/` | Indefinite | No |
| Compliance audit log | `/data/compliance/audit.jsonl` | 90 days | No |

Data that "can leave the device" means it may be sent to a cloud AI provider
as part of an inference request. All such egress is subject to the residency
policy below.

### What is in a session?

Each session file (`/data/sessions/<id>.json`) contains:
- The session ID and timestamps
- The full turn-by-turn transcript (user messages and model responses)
- Tool call inputs and outputs
- The provider name that handled each turn

Sessions do NOT contain raw API keys or passwords. These are redacted at
creation time by the agent runtime.

---

## Data-residency policy

The residency policy is stored at `/data/compliance/policy.json`. It controls
which AI providers may handle which turns.

### Default policy

```json
{
  "allow_cross_border": false,
  "cross_border_must_be_logged": true,
  "retention_days": 90,
  "providers": [
    { "name": "local",     "residency": "local",        "allow_sensitive": true  },
    { "name": "anthropic", "residency": "cross-border",  "allow_sensitive": false },
    { "name": "openai",    "residency": "cross-border",  "allow_sensitive": false }
  ]
}
```

By default:

- Only the local provider is used (`allow_cross_border: false`).
- Cloud providers are classified as `cross-border` and are not permitted.
- Sensitive turns cannot be routed to cloud providers even if cross-border is
  enabled.

### Enabling cross-border egress

To allow cloud providers, set `allow_cross_border: true` in the policy file
and ensure `cross_border_must_be_logged: true` so every egress turn is written
to the audit log before being sent.

```json
{
  "allow_cross_border": true,
  "cross_border_must_be_logged": true,
  "providers": [
    { "name": "local",     "residency": "local",       "allow_sensitive": true  },
    { "name": "anthropic", "residency": "cross-border", "allow_sensitive": false }
  ]
}
```

### Sovereign providers

To designate a sovereign provider (e.g. an EU-hosted endpoint):

```json
{
  "providers": [
    { "name": "eu-endpoint", "residency": "sovereign", "allow_sensitive": true }
  ]
}
```

Sovereign providers are permitted when `allow_cross_border: false` as long as
they are listed explicitly.

---

## Compliance audit log

Every AI provider routing decision is written to
`/data/compliance/audit.jsonl` as a JSON line. Each line contains:

```json
{
  "turn_id": "t-abc123",
  "provider": "local",
  "residency": "local",
  "sensitive": false,
  "cross_border": false,
  "at": "2026-06-15T10:00:00Z"
}
```

### Viewing the report

```sh
nuractl data compliance-report
```

Output:

```
TURN_ID                               PROVIDER      RESIDENCY     SENSITIVE  CROSS_BORDER
t-abc123                              local         local         false      false
t-def456                              anthropic     cross-border  false      true
```

JSON output:

```sh
nuractl data compliance-report --json
```

---

## Data deletion

### Automatic retention enforcement

The agent enforces the retention policy automatically. Data older than
`retention_days` is eligible for deletion. To trigger deletion manually:

```sh
# Delete expired data (default 90-day retention):
nuractl data delete-expired

# Custom retention window:
nuractl data delete-expired --retention-days 30
```

This command removes files from `/data/sessions/`, `/data/journal/`, and
`/data/provenance/` that were last modified more than `retention_days` ago.
Crash captures are rotated separately (see
[operating.md](operating.md#crash-diagnostics)).

### On-demand deletion

To delete all user data immediately (GDPR right-to-erasure):

```sh
# Stop all services first:
nuractl stop gateway
nuractl stop llama-server

# Delete data directories:
rm -rf /data/sessions /data/journal /data/provenance /data/compliance

# Restart:
nuractl start llama-server
nuractl start gateway
```

Model files and configuration are not deleted by the above. To factory-reset
the full appliance, re-flash the rootfs image.

---

## What can leave the device

Under the default policy, nothing leaves the device. When cross-border egress
is enabled:

| What | Goes to | Conditions |
|------|---------|------------|
| Turn prompt (user message + history) | Cloud provider | `allow_cross_border=true`, `sensitive=false` |
| Model response | Returned from cloud provider | Same |
| Provenance metadata | Written locally to audit log | Always (before egress) |

API keys are transmitted to the provider's endpoint as HTTP headers but are
never written to local files.

---

## Summary

NuraOS is designed for local-first operation. The default configuration never
sends data to external services. Cross-border egress requires explicit policy
changes, is always logged, and can be scoped by residency class. The compliance
audit log provides a verifiable per-turn record of which provider handled which
turn.
