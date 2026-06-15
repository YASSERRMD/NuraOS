# NuraOS Operator Handbook

This handbook covers the full operational lifecycle of NuraOS: installation,
first boot, provider switching, updates, rollback, backup/restore, recovery,
and incident response. Each section is a self-contained runbook with
step-by-step commands.

All paths are relative to the appliance unless marked `[HOST]`.

---

## 1. Installation

### Prerequisites

- x86-64 machine or QEMU (recommended: 4 vCPUs, 4 GB RAM, 8 GB disk)
- QEMU: [host-setup.md](host-setup.md)
- Build tools: `scripts/` in this repository

### Steps

```sh
# 1. Build the kernel, rootfs, and UEFI image [HOST]
scripts/build-kernel.sh
scripts/build-rootfs.sh
scripts/build-gateway.sh
scripts/build-nuractl.sh

# 2. Write the image to a disk or start QEMU [HOST]
# QEMU (recommended for first install):
scripts/run-qemu.sh

# 3. First boot: the system boots to the serial REPL automatically.
# Serial console: 115200 baud, no parity, 8 bits, 1 stop bit.
```

See [boot-chain.md](boot-chain.md) for the full boot sequence.

---

## 2. First Boot

On first boot the system creates a stable machine identity and starts the
gateway. Verify readiness:

```sh
# Run the boot self-test suite:
nuractl selftest --boot

# Check all services are running:
nuractl list

# Verify the gateway is responsive:
curl -sf http://127.0.0.1:8080/healthz && echo OK
```

If `selftest --boot` reports any failures, see the
[Troubleshooting guide](#7-troubleshooting-guide).

---

## 3. Switching Providers

NuraOS supports local (llama.cpp), Anthropic, and OpenAI providers.

```sh
# Check current provider:
curl -sf http://127.0.0.1:8080/config | grep provider

# Switch provider per-request:
curl -sf -X POST http://127.0.0.1:8080/chat \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hi"}],"provider":"anthropic"}'
```

Set a permanent default by updating `/data/etc/agent.toml`:

```toml
[provider]
active = "anthropic"   # or "openai" or "local"
```

Then restart the agent:

```sh
nuractl restart nura-agent
```

Provider credentials go in `/data/etc/secrets.toml` (mode 0600):

```toml
anthropic_api_key = "sk-ant-..."
openai_api_key    = "sk-..."
```

See [providers.md](providers.md) for the full provider resilience model.

---

## 4. Applying an Update

```sh
# 1. Download the new image to the appliance:
scp nuraos-v2.img appliance:/tmp/

# 2. Verify integrity and apply:
nuractl update apply /tmp/nuraos-v2.img --sha256 <expected-sha256>

# 3. Confirm the update committed:
nuractl update log

# 4. Reboot to activate the new slot:
nuractl reboot
```

The update writes to the inactive A/B slot. If the new image fails to boot
(counted by the boot counter), the bootloader automatically rolls back to
the previous slot.

See [update.md](update.md) and [updates.md](updates.md) for details.

---

## 5. Rolling Back

### After a failed update

If the system automatically rolled back (boot counter expired):

```sh
# Confirm active slot:
nuractl update log

# The previous slot is now active; no action needed.
```

### Manual rollback

```sh
# List version history:
nuractl history list

# Roll back to a specific entry:
nuractl history rollback <entry-id>

# Reboot to activate:
nuractl reboot
```

### Config rollback

```go
// Go API:
s := configmgr.NewStore("/data/config")
s.RollbackTo(2) // restore config version 2
```

See [configmgr](../services/internal/configmgr/) and the config doc
([config.md](config.md)) for details.

---

## 6. Backup and Restore

### Creating a backup

```sh
# Standard backup (models excluded):
nuractl backup --out /mnt/usb/backup-$(date +%Y%m%d).tar.gz

# Encrypted backup:
nuractl backup --out /mnt/usb/backup.tar.gz --passphrase "strong-passphrase"

# Quiesce services first for maximum consistency:
nuractl stop gateway
nuractl stop llama-server
nuractl backup --out /mnt/usb/backup.tar.gz
nuractl start llama-server
nuractl start gateway
```

### Restoring

```sh
# Preview what would be restored (dry run):
nuractl restore /mnt/usb/backup.tar.gz --dest /data --dry-run

# Restore with SHA-256 verification:
nuractl restore /mnt/usb/backup.tar.gz --dest /data --sha256 <hash>

# Restore encrypted backup:
nuractl restore /mnt/usb/backup.tar.gz --dest /data --passphrase "strong-passphrase"
```

See [operating.md](operating.md) for backup/restore documentation.

---

## 7. Troubleshooting Guide

### Self-test failures

Run the full self-test to identify the failing subsystem:

```sh
nuractl selftest
```

| Check | Failure | Resolution |
|-------|---------|------------|
| `rng` | entropy < 64 bits | Attach `virtio-rng` device; ensure seed is loaded |
| `cgroups` | cgroup v2 not mounted | Boot with `systemd.unified_cgroup_hierarchy=1` or mount manually |
| `namespaces` | `/proc/self/ns/mnt` missing | Ensure `CONFIG_NAMESPACES=y` in kernel config |
| `seccomp` | not detected | Ensure `CONFIG_SECCOMP=y` and `CONFIG_SECCOMP_FILTER=y` |
| `storage` | write failed on /data | Check `/data` is mounted and writable (`mount | grep data`) |
| `network` | loopback not found | Bring up `lo`: `ip link set lo up; ip addr add 127.0.0.1/8 dev lo` |

### Services not starting

```sh
# Check manager socket is alive:
nuractl list

# Check specific service status:
nuractl status gateway

# View recent logs:
nuractl logs gateway -n 100
```

### Gateway unreachable

```sh
# Verify it is listening:
ss -tlnp | grep 8080

# Check auth: if bearer token is set, include it:
curl -sf -H 'Authorization: Bearer <token>' http://127.0.0.1:8080/healthz

# Check rate limiting: look for 429 responses in the metrics:
curl -sf http://127.0.0.1:8080/metrics | grep rate_limited
```

### Crash diagnostics

```sh
# List recent crash captures:
ls /data/crashes/

# Bundle a redacted diagnostic archive:
nuractl diag --out /tmp/

# Check for kernel panic records (after an unexpected reboot):
ls /sys/fs/pstore/dmesg-* 2>/dev/null && echo "panic record found"
```

See [operating.md](operating.md) crash diagnostics section.

### Disk space

```sh
# Check disk usage:
nuractl status gateway   # look for disk_pct in /status endpoint

# Reclaim space:
nuractl reclaim
```

### Provider failures

```sh
# Check provider circuit breaker state:
curl -sf http://127.0.0.1:8080/status | grep circuit

# Check metrics for failure rate:
curl -sf http://127.0.0.1:8080/metrics | grep nura_provider
```

---

## 8. Incident Response

### Service crash loop

```sh
# 1. Identify the crashing service:
nuractl list

# 2. Check crash captures:
ls /data/crashes/ | tail -5
cat /data/crashes/<latest>.json

# 3. Temporarily disable the service to stop the loop:
nuractl disable <service>
nuractl stop <service>

# 4. Gather diagnostics:
nuractl diag --out /tmp/incident-$(date +%Y%m%d).tar.gz

# 5. Fix the root cause, then re-enable:
nuractl enable <service>
nuractl start <service>
```

### Full system hang

If the system is unresponsive but has network access:

```sh
# Check if the gateway is alive (from host or another machine):
curl -sf http://<appliance-ip>:8080/healthz

# If unresponsive for > 30 s, the watchdog should have triggered a reset.
# After the reset, check pstore for the panic record:
nuractl diag
```

If the watchdog did not trigger a reset, verify the watchdog device is
enabled (see [resilience.md](resilience.md)).

### Compromised secrets

```sh
# 1. Immediately rotate secrets:
vi /data/etc/secrets.toml   # update keys
chmod 0600 /data/etc/secrets.toml

# 2. Restart the agent to pick up new secrets:
nuractl restart nura-agent

# 3. Verify old keys are no longer used:
curl -sf -H 'Authorization: Bearer <new-token>' http://127.0.0.1:8080/healthz
```

---

## 9. Reference

| Command | Description |
|---------|-------------|
| `nuractl list` | List all services |
| `nuractl status <svc>` | Detailed service status |
| `nuractl start/stop/restart <svc>` | Lifecycle control |
| `nuractl logs <svc> [-n N]` | Last N log lines |
| `nuractl enable/disable <svc>` | Persistent enable/disable |
| `nuractl selftest [--boot] [--category X]` | Built-in health checks |
| `nuractl diag [--out DIR]` | Redacted diagnostic archive |
| `nuractl backup --out FILE` | Backup /data |
| `nuractl restore FILE` | Restore from backup |
| `nuractl update apply FILE` | Apply firmware update |
| `nuractl update rollback` | Roll back last update |
| `nuractl history list` | Version history |
| `nuractl history rollback ID` | Restore a history entry |
| `nuractl reclaim` | Free disk space |
| `nuractl poweroff` | Graceful shutdown |
| `nuractl reboot` | Graceful reboot |
| `nuractl events` | Tail system events |
| `nuractl pkg install FILE` | Install a signed package |
| `nuractl pkg list` | List installed packages |
| `nuractl pkg remove NAME` | Remove a package |
