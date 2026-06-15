# NuraOS Security Model

NuraOS is designed as a local-first appliance running inside a QEMU VM on a
trusted machine. This document describes the threat model, the protections in
place, and guidance for safe LAN exposure.

## Threat model

**In scope (protected)**:
- Unauthorised access from the LAN when the gateway is exposed.
- Request floods and resource exhaustion from misbehaving clients.
- MIME-type confusion and clickjacking via browser-level attacks.
- Information leakage of provider API keys through logs or metrics.

**Out of scope (not protected at this layer)**:
- Physical access to the host machine (mitigated by optional /data encryption).
- A compromised QEMU host OS.
- Traffic interception in transit (no TLS; see below).
- A malicious model payload loaded from `/data/models/`.

## Default configuration

By default the gateway binds to `127.0.0.1:8080` and is accessible only from
the host running QEMU. No bearer token is required. This is the safe default
for single-user development.

```
Host: curl http://localhost:18080/healthz
```

## Enabling LAN exposure (opt-in)

To expose the gateway on all interfaces (e.g. a home server), set:

```sh
GATEWAY_BIND_LAN=1
```

**Before doing so, configure a bearer token** (see below), or any device on
the LAN can query your AI appliance and read files accessible to the agent.

## Bearer-token authentication

Create `/data/etc/secrets.toml` inside the guest:

```sh
mkdir -p /data/etc
chmod 700 /data/etc
cat > /data/etc/secrets.toml <<'EOF'
gateway_token = "replace-with-a-long-random-string"
EOF
chmod 600 /data/etc/secrets.toml
```

Generate a strong token:
```sh
head -c 32 /dev/urandom | base64 | tr -d '=/+'
```

When `gateway_token` is set, all endpoints except `GET /healthz` require:
```
Authorization: Bearer <your-token>
```

Unauthenticated requests receive `401 Unauthorized`.

Example curl with auth:
```sh
curl -H "Authorization: Bearer <token>" http://host:18080/tools
```

## Rate limiting and concurrency

The gateway enforces two independent limits to prevent resource exhaustion:

| Limit | Default | Scope |
|-------|---------|-------|
| Requests per second | 1 req/s (burst 10) | Per client IP |
| Concurrent requests | 4 | Global (all clients) |

Requests exceeding either limit receive `429 Too Many Requests` with a
`Retry-After: 1` header. The `/healthz` endpoint is exempt from both limits
so monitoring tools always get a response.

## Security response headers

All responses include:

| Header | Value | Purpose |
|--------|-------|---------|
| `X-Content-Type-Options` | `nosniff` | Prevent MIME sniffing |
| `X-Frame-Options` | `DENY` | Prevent clickjacking |
| `Content-Security-Policy` | `default-src 'none'` | No resource loading |

## Transport security

The gateway does NOT implement TLS. For LAN exposure with encryption:

1. Run a TLS-terminating reverse proxy (nginx, caddy) on the host.
2. Forward decrypted traffic to the QEMU port forward (e.g. `localhost:18080`).
3. Clients connect to the proxy over HTTPS.

Example nginx snippet:
```nginx
server {
    listen 443 ssl;
    ssl_certificate     /etc/ssl/nura.crt;
    ssl_certificate_key /etc/ssl/nura.key;
    location / {
        proxy_pass http://127.0.0.1:18080;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

## Secrets management

### Permissions enforcement

The agent aborts startup if `/data/etc/secrets.toml` is group- or
world-readable (mode bits `0o044` set). The error message includes the
remediation step:

```
secrets file /data/etc/secrets.toml is group- or world-readable;
run 'chmod 600 /data/etc/secrets.toml' and restart
```

Create the file with the correct permissions from the start:

```sh
install -m 600 /dev/null /data/etc/secrets.toml
echo 'gateway_token = "replace-with-long-random-string"' >> /data/etc/secrets.toml
```

### Environment-variable overrides

Secrets may be supplied entirely via environment variables, avoiding a file
on disk:

| Variable | Overrides |
|----------|-----------|
| `ANTHROPIC_API_KEY` | `anthropic_api_key` in secrets.toml |
| `OPENAI_API_KEY` | `openai_api_key` in secrets.toml |
| `NURA_GATEWAY_TOKEN` | `gateway_token` in secrets.toml |

Environment variables take precedence over file values. When both are present
the env var wins.

### Token rotation without restart (gateway)

The gateway token can be rotated live without a full restart:

1. Update `/data/etc/secrets.toml` with the new `gateway_token` value.
2. Send `SIGHUP` to the gateway process:
   ```sh
   kill -HUP $(pidof gateway)
   ```
3. The gateway reloads the secrets file and begins enforcing the new token
   immediately. In-flight requests are not interrupted.

The supervisor (or the operator) can invalidate the old token and begin
issuing the new one once the SIGHUP is confirmed via the log line:
`"gateway token reloaded from secrets file"`.

### What secrets must never enter

- Logs (any level): `SecretString` redacts in `Debug` and `Display`.
- Metrics labels or values: no secret-derived label is emitted.
- Provenance log: content fields never include raw key material.
- Crash dumps or panic output: `SecretString` has no raw-value `Drop`.

## Constant-time token comparison

The bearer-token check uses `crypto/subtle.ConstantTimeCompare` to prevent
timing-based token enumeration:

```go
subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1
```

A naive `!=` comparison short-circuits on the first differing byte, which
leaks information about where tokens diverge. Constant-time comparison takes
the same number of cycles regardless of the mismatch position.

In practice the rate limiter (1 RPS per IP) already prevents bulk timing
measurements, but the constant-time comparison removes the theoretical risk.

## Security posture check

Run before every release:

```sh
./scripts/security-check.sh
```

The script verifies: no hardcoded secret patterns in tracked files,
`secrets.toml` not tracked in git, constant-time auth, security headers,
rate-limit and concurrency middleware, `ReadTimeout`, and `MaxBytesReader`.

The CI pipeline runs this check on every pull request (see `ci.yml`).

## Attack surface summary

| Attack | Mitigation |
|--------|-----------|
| Unauth API calls | Bearer token; disabled by default when no token set |
| Token timing attack | `crypto/subtle.ConstantTimeCompare` |
| DDoS / request flood | Per-IP rate limit + global concurrency cap |
| MIME confusion | `X-Content-Type-Options: nosniff` |
| Clickjacking | `X-Frame-Options: DENY` |
| Large request bodies | 64 KiB cap on POST /chat |
| Slow-client attacks | `ReadTimeout: 10 s` on the HTTP server |
| Path traversal in tools | `fs.read` rejects `..` components lexically |
| Root process escape | nura-agent and gateway run as uid=1000 |
| Hardcoded secrets | CI secrets-scan job on every PR |
| Physical access to disk image | Optional LUKS2 encryption for /data (nura.data.luks=1) |
| Lost key / no passphrase | Fail closed: no data mounted, emergency shell only |

---

## /data encryption at rest (optional)

Encrypting the `/data` partition protects against an attacker who gains
physical access to the QEMU disk image file. Without encryption, copying
`data.img` off the host is sufficient to read secrets, session history, and
model prompts.

### Encryption model

NuraOS uses **LUKS2** (Linux Unified Key Setup v2) with dm-crypt to provide
full-volume encryption for `/data`. The default plain-text mode is unchanged;
encryption is opt-in via a kernel cmdline flag.

| Property | Value |
|----------|-------|
| Cipher | LUKS2 default (aes-xts-plain64, 256-bit key) |
| KDF | argon2id (LUKS2 default; memory-hard) |
| Key slots | up to 32; slot 0 = key file / passphrase, slot 1 = recovery passphrase |
| Metadata overhead | 16 MiB LUKS2 header at front of image |

### Enabling encryption

On the host, before first boot:

```sh
# Format the data image with LUKS (passphrase prompted):
sudo ./scripts/setup-luks.sh

# Or generate a key file for automatic unlock:
sudo ./scripts/setup-luks.sh --key-file key.bin
```

Then add `nura.data.luks=1` to the kernel cmdline. With `run-qemu.sh`, edit
the `-append` line in the script:

```sh
-append "... nura.data.luks=1"
```

### Key source options and trade-offs

| Source | How | Trade-off |
|--------|-----|-----------|
| **Key file on secondary block device** | Attach as a second virtio disk (`-drive file=key.bin,...`) | Automatic unlock; physical key device must be present at boot |
| **Passphrase on serial console** | Typed on `/dev/console` at boot | No extra hardware; requires manual intervention at every boot |

The two key sources are tried in order. If neither succeeds, the system fails
closed (no `/data` mount, no services start, emergency shell).

### What lives on the encrypted volume

All NuraOS application data lives under `/data`. When encryption is enabled,
the following are protected at rest:

| Path | Sensitive content |
|------|------------------|
| `/data/etc/secrets.toml` | Provider API keys, gateway bearer token |
| `/data/etc/agent.toml` | Agent configuration |
| `/data/sessions/` | Full conversation history and tool call records |
| `/data/journal/` | Structured log records (may contain user prompts) |
| `/data/models/` | Model weights (not secrets, but may be licensed) |

The initramfs and kernel are NOT encrypted. A determined attacker with host
access can modify `/init` to bypass LUKS. Full measured boot (TPM attestation)
is a future phase.

### Graceful failure (fail closed)

If the key is unavailable at boot:
- `/data` is NOT mounted in plain text
- No services start
- `/init` logs a clear message and drops to an emergency shell:
  ```
  [init] LUKS: all key sources exhausted; /data partition locked
  [init] LUKS: boot with nura.recovery=1 to access a recovery shell
  [init] LUKS: failing closed -- no persistent data will be accessible
  ```

To recover: attach the correct key device and reboot, or enter the recovery
passphrase when prompted.

### Setup script reference

```sh
# Format (passphrase):
sudo ./scripts/setup-luks.sh --image image/out/data.img

# Format (key file, auto-unlock):
sudo ./scripts/setup-luks.sh --key-file secrets/key.bin

# Custom size:
sudo ./scripts/setup-luks.sh --size 4096 --key-file secrets/key.bin
```

The script creates the ext4 filesystem inside the LUKS container and
initialises the expected subdirectories (`models/`, `etc/`, etc.).

### Key management guidance

- Store the key file on a dedicated USB drive or in a password manager.
- The key file is 4096 random bytes; losing it loses all `/data` content.
- Always add a recovery passphrase slot (`cryptsetup luksAddKey`) as a backup.
- Do NOT commit key files to git. Add `*.bin` and `secrets/` to `.gitignore`.
- Rotate keys periodically: `cryptsetup luksChangeKey`.

---

## Least-privilege model (Phase 41)

### Service accounts

| Account | uid | gid | Shell        | Purpose                  |
|---------|-----|-----|--------------|--------------------------|
| root    | 0   | 0   | /bin/sh      | PID 1 supervisor only    |
| nura    | 1000| 1000| /bin/false   | nura-agent and gateway   |

The `nura` account has no login shell and no password. Only `root`
(the supervisor) can `su` to it.

### Ownership layout

| Path           | Owner    | Mode | Notes                              |
|----------------|----------|------|------------------------------------|
| /run/          | nura     | 750  | Unix socket for IPC lives here     |
| /data/logs/    | nura     | 750  | Service log files                  |
| /data/sessions/| nura     | 750  | Conversation session data          |
| /data/models/  | root     | 755  | Model files (read-only for nura)   |
| /data/etc/     | root     | 755  | Config files (read-only for nura)  |
| /sbin/         | root     | 755  | Binaries owned by root             |

### Privilege drop sequence

1. `/init` (root) mounts filesystems and sets up ownership via `chown 1000:1000`.
2. The supervisor (root) starts each service via `su -s /bin/sh nura -c "exec /sbin/<svc>"`.
3. `su` drops to uid=1000, gid=1000 before exec-ing the binary.
4. Neither nura-agent nor gateway ever runs with any capabilities beyond what uid=1000 has.

If `su` is absent or the `nura` account is missing at boot, services fall back
to running as root with a logged warning.
