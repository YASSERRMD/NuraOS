# NuraOS Operating Guide

This document covers day-to-day operation of a running NuraOS appliance using
the `nuractl` CLI.

---

## nuractl

`nuractl` communicates with `nura-manager` over a Unix domain control socket
at `/run/nura-manager.sock`. The socket is mode 0600 (owner root only); run
`nuractl` as root or via an authorised shell.

### Commands

```sh
nuractl list                    # list all services and their states
nuractl status <service>        # detailed status for one service
nuractl start  <service>        # request start
nuractl stop   <service>        # request stop
nuractl restart <service>       # request restart
nuractl logs   <service>        # last 50 log lines
nuractl logs   <service> -n 200 # last 200 log lines
nuractl enable  <service>       # mark service enabled
nuractl disable <service>       # mark service disabled
```

### Flags

| Flag | Description |
|---|---|
| `--json` | Emit JSON instead of human-readable text |
| `--socket PATH` | Connect to a non-default manager socket |

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Error (message printed to stderr) |

### Example: list services

```
$ nuractl list
NAME                  STATE       RESTARTS  SINCE
------------------------------------------------------------
llama-server          running     0         2026-06-15T08:00:00Z
nura-agent            running     0         2026-06-15T08:00:05Z
gateway               running     0         2026-06-15T08:00:12Z
```

### Example: JSON output for scripting

```sh
nuractl --json status gateway | jq .service.state
```

---

## Service states

| State | Meaning |
|---|---|
| `inactive` | Not started or fully stopped |
| `starting` | Process launched; readiness probe pending |
| `ready` | Readiness probe passed; dependants may start |
| `running` | Process live; no active probe |
| `stopping` | SIGTERM sent; drain period active |
| `failed` | Exited and restart policy is `no` |

---

## Control socket protocol

`nuractl` sends one JSON line per request and reads one JSON line response.
The connection is closed after each request/response pair.

Request schema:

```json
{"command": "list"}
{"command": "status",  "service": "gateway"}
{"command": "start",   "service": "gateway"}
{"command": "stop",    "service": "gateway"}
{"command": "restart", "service": "gateway"}
{"command": "logs",    "service": "gateway", "lines": 50}
{"command": "enable",  "service": "gateway"}
{"command": "disable", "service": "gateway"}
```

Response schema:

```json
{"ok": true,  "services": [...]}
{"ok": true,  "service": {...}}
{"ok": true,  "message": "stopped gateway"}
{"ok": false, "error": "unknown service: foo"}
```

---

## Common tasks

### Check if the gateway is up

```sh
nuractl status gateway
```

### Restart the agent after config change

```sh
nuractl restart nura-agent
```

### View recent gateway logs

```sh
nuractl logs gateway -n 100
```

Note: full log integration ships in Phase 60 (journal). Until then, `logs`
returns a placeholder message.

### Disable a service across reboots

```sh
nuractl disable gateway
# edit /etc/nura/services/gateway.toml: set enabled = false
# then restart the manager for the change to take effect
```

---

## Self-test suite

NuraOS ships a built-in health check suite (`selftest`) that verifies kernel
features, storage durability, and network posture. Checks are categorised as
`kernel`, `storage`, `network`, or `agent`. A subset of checks is designated
the _boot set_ and runs automatically at startup to gate system readiness.

### Running all checks

```sh
nuractl selftest
```

Example human-readable output:

```
[OK  ] kernel       cgroups    cgroup v2 with cpu and memory controllers (2ms)
[OK  ] kernel       namespaces mount and PID namespaces available (0ms)
[OK  ] kernel       rng        4096 bits available (0ms)
[OK  ] kernel       seccomp    seccomp BPF with kill_process action (0ms)
[OK  ] network      firewall   nftables firewall active (1ms)
[OK  ] network      network    loopback stack operational (connection refused as expected) (3ms)
[OK  ] storage      storage    write/fsync/read verified on /data (5ms)

Overall: pass  pass=7 fail=0 skip=0 (11ms)
```

### Boot-set only

Runs only the minimal subset that gates system readiness:

```sh
nuractl selftest --boot
```

### Filtering by category

```sh
nuractl selftest --category kernel
nuractl selftest --category storage
nuractl selftest --category network
```

### JSON output

```sh
nuractl selftest --json
```

Returns a JSON object with `results`, `pass`, `fail`, `skip`, `overall`, and
`elapsed_ms`. Exit code is 2 when any check fails, 0 when all pass or skip.

### Check catalogue

| Name | Category | Boot | What it checks |
|------|----------|------|----------------|
| `rng` | kernel | yes | `/proc/sys/kernel/random/entropy_avail` >= 64 bits |
| `cgroups` | kernel | yes | cgroup v2 mounted; `cpu` and `memory` controllers present |
| `namespaces` | kernel | no | `/proc/self/ns/mnt` and `/proc/self/ns/pid` visible |
| `seccomp` | kernel | no | `/proc/sys/kernel/seccomp/actions_avail` readable |
| `storage` | storage | yes | write + fsync + read a 32-byte test file on `/data` |
| `network` | network | no | loopback interface up; TCP stack reachable on 127.0.0.1 |
| `firewall` | network | no | nftables conntrack or iptables rules detected |

Checks skip gracefully on non-Linux platforms. The `storage` check falls back
to `$TMPDIR` when `/data` is not mounted (useful in CI environments).

---

## Crash diagnostics

When a service exits unexpectedly, the NuraOS manager (or the crash-cap
library used by the manager) writes a bounded, redacted diagnostic capture
to `/data/crashes/`. Each capture is a JSON file named
`<service>-<timestamp>.json` containing the last log lines, exit code,
and lightweight resource state (RSS, open FDs, cgroup slice).

Secrets are scrubbed before any data touches disk. Recognised patterns:
API keys, bearer tokens, passwords, private-key PEM blocks, and long hex
strings. The placeholder `[REDACTED]` replaces every match.

At most 20 captures are retained (configurable via `MaxBundles` in the
`crashcap` package). Oldest files are rotated out automatically.

### Kernel panic capture

On first boot after a kernel panic, the kernel writes a crash record to
`/sys/fs/pstore/` (requires `CONFIG_PSTORE` and either ramoops or EFI
pstore). NuraOS reads, redacts, and archives these records to
`/data/crashes/` during startup via the `paniccap` package.

To check if pending panic records exist:

```sh
ls /sys/fs/pstore/dmesg-* 2>/dev/null && echo "panic records found"
```

To trigger archival manually (happens automatically on startup):

```sh
nuractl diag
```

### Bundling a diagnostic archive

`nuractl diag` reads all files from `/data/crashes`, re-applies redaction,
and writes a gzip-compressed tar archive suitable for offline analysis:

```sh
# Bundle to /tmp (default):
nuractl diag

# Bundle to a specific directory:
nuractl diag --out /mnt/usb

# JSON output (returns archive path):
nuractl diag --json
```

The archive is named `nura-diag-<timestamp>.tar.gz`. It contains at most
50 files from `/data/crashes`. Kernel panic records archived from pstore
during this run are also included.

Exit code is 0 on success. The command fails if `/data/crashes` does not
exist.

---

## Backup and restore

NuraOS backs up the entire `/data` directory (config, sessions, journal, crash
captures) to a gzip-compressed tar archive. Large model blobs are excluded by
default to keep backup sizes manageable.

### Creating a backup

```sh
# Minimal backup (models excluded, no encryption):
nuractl backup --out /mnt/usb/nura-backup.tar.gz

# Include model blobs:
nuractl backup --out /mnt/usb/nura-backup.tar.gz --include-models

# Encrypted backup (AES-256-GCM, key derived from passphrase):
nuractl backup --out /mnt/usb/nura-backup.tar.gz --passphrase "my-secret"

# JSON output (returns path and SHA-256):
nuractl backup --out /tmp/backup.tar.gz --json
```

The command writes a sidecar manifest (`<archive>.manifest.json`) alongside the
archive containing the SHA-256 digest, file count, creation timestamp, and
encryption flag.

Model blob policy: files under `/data/models/**` are excluded by default.
Pass `--include-models` to override. For large deployments, back up models
separately or rely on re-download.

### Restoring from a backup

```sh
# Dry-run (list what would be restored without writing):
nuractl restore /mnt/usb/nura-backup.tar.gz --dest /data --dry-run

# Restore with SHA-256 verification:
nuractl restore /mnt/usb/nura-backup.tar.gz --dest /data \
    --sha256 <hex-from-manifest>

# Restore from encrypted backup:
nuractl restore /mnt/usb/nura-backup.tar.gz --dest /data \
    --passphrase "my-secret"
```

Restore aborts if the SHA-256 does not match the expected value (tamper
detection). For encrypted archives, decryption failure also aborts the restore.

### Consistency

The backup command walks the live `/data` directory. For maximum consistency,
quiesce dependent services before running a backup:

```sh
nuractl stop gateway
nuractl stop llama-server
nuractl backup --out /mnt/usb/nura-backup.tar.gz
nuractl start llama-server
nuractl start gateway
```

---

## Build

```sh
scripts/build-nuractl.sh      # builds rootfs/staging/sbin/nuractl
scripts/build-initramfs.sh    # includes nuractl in the initramfs
```
