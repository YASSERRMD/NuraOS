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

## Least-privilege model

### Service accounts

Each service runs as its own unprivileged account. The service manager (nura-manager)
runs as root during boot so it can set hostnames, mount filesystems, and spawn child
processes as different users (requires CAP_SETUID). This is the only process justified
to run as root in steady state.

| Account  | UID  | GID  | Shell       | Runs                        |
|----------|------|------|-------------|----------------------------|
| root     | 0    | 0    | /bin/sh     | PID 1 init + service manager |
| nura-mgr | 100  | 100  | /bin/false  | (reserved for future use)   |
| nura     | 1000 | 1000 | /bin/false  | nura-agent (AI agent)       |
| nura-gw  | 1001 | 1001 | /bin/false  | nura-gateway (HTTP gateway) |
| llama    | 1002 | 1002 | /bin/false  | llama-server (inference)    |

No account has a password or login shell. `/etc/shadow` is mode 640 (root-readable only).

### Ownership layout

| Path             | Owner       | Mode | Reason                                    |
|------------------|-------------|------|-------------------------------------------|
| /run/            | root        | 755  | Service manager creates sockets here      |
| /data/journal/   | root        | 755  | Manager writes journal on behalf of all   |
| /data/logs/      | nura (1000) | 750  | Agent writes log files                    |
| /data/sessions/  | nura (1000) | 750  | Agent writes session provenance           |
| /data/models/    | root:llama  | 750  | llama-server reads models; no other write |
| /data/etc/       | root        | 755  | Config readable by all services           |
| /sbin/           | root        | 755  | OS binaries; no service writes here       |

### Privilege drop sequence

1. `/init` (root) mounts filesystems and sets directory ownership.
2. The supervisor (root) launches `nura-manager run` as root.
3. `nura-manager` reads unit files and for each service calls
   `su -s /bin/sh <user> -c "exec /sbin/<binary>"` before exec'ing the process.
4. `su` drops to the service's UID/GID before the binary starts.
5. In steady state: nura-agent runs as uid=1000, nura-gateway as uid=1001,
   llama-server as uid=1002. No network-facing service runs as root.

If `su` is absent or a service account is missing at boot, the service falls back
to running as root with a logged warning.

### Steady-state audit

```
nura-manager run   PID ?   UID 0    (justified: needs CAP_SETUID to spawn children)
nura-agent         PID ?   UID 1000 (no capabilities)
nura-gateway       PID ?   UID 1001 (no capabilities)
llama-server       PID ?   UID 1002 (no capabilities)
```

---

## Syscall filtering (seccomp)

Each service runs under a BPF allowlist that restricts it to the syscalls it
actually needs. The service manager (`nura-manager`) installs the filter via
`prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER)` before exec'ing the service.
The filter is inherited across all subsequent exec calls in the privilege-drop
chain (`nura-manager seccomp-exec -> su -> sh -> service`).

### Modes

| Mode | Default action | Use case |
|------|---------------|----------|
| `enforce` | `SECCOMP_RET_ERRNO\|EPERM` | Production; denied syscalls return an error |
| `log` | `SECCOMP_RET_LOG` (kernel audit) | Profile development; all syscalls allowed but unmatched ones are logged |

Run with `mode = "log"` first, collect unmatched syscalls from `/var/log/audit.log`
or `dmesg | grep seccomp`, add them to the profile, then switch to `mode = "enforce"`.

### Profile format

Profiles live in `/etc/nura/seccomp/<service>.toml`:

```toml
mode = "enforce"   # optional; overrides the unit-level mode

syscalls = [
  "read", "write", "openat", "close",
  # ... additional allowed syscalls
]
```

All syscall names use the Linux kernel ABI names (lowercase, no `sys_` prefix).
Unknown names cause the manager to refuse to start the unit.

### Per-service profile locations

| Service | Profile | Notes |
|---------|---------|-------|
| nura-agent | `/etc/nura/seccomp/nura-agent.toml` | Includes UNIX socket + HTTP client syscalls |
| gateway | `/etc/nura/seccomp/gateway.toml` | Includes TCP accept + UNIX socket client syscalls |
| llama-server | `/etc/nura/seccomp/llama-server.toml` | Includes `mmap` for model weight loading + pthread syscalls |

Each profile also covers the BusyBox `su` + `sh` launch chain (`execve`, `setresuid`,
`setresgid`, `capget`, `capset`, etc.). These entries can be removed if the privilege
drop mechanism changes to `SysProcAttr.Credential` in a future phase.

### Unit configuration

```toml
[seccomp]
profile = "/etc/nura/seccomp/nura-agent.toml"
mode    = "enforce"
```

### Architecture check

The BPF program loads the `arch` field of `struct seccomp_data` and kills the
process with `SECCOMP_RET_KILL_PROCESS` if the architecture is not
`AUDIT_ARCH_X86_64`. This prevents a confused 32-bit ABI from bypassing the
64-bit allowlist (x32 ABI is disabled on this kernel).

### Extending a profile

1. Set `mode = "log"` in the service's `[seccomp]` section and restart.
2. Observe the audit log for denied entries:
   ```sh
   dmesg | grep 'seccomp'
   # or: ausearch -m SECCOMP -ts recent
   ```
3. Add the missing syscall names to the profile TOML.
4. Set `mode = "enforce"` and restart to re-enable the filter.

### Attack surface reduction

| Before | After |
|--------|-------|
| ~340 available syscalls (x86-64) | 70-100 per service |
| Any exploit could pivot via arbitrary syscalls | Blocked syscalls return EPERM; no kernel code path reached |
| Kernel 0-day (e.g. nft, io_uring) exploitable | Kernel subsystem not reachable if its syscall is not in the allowlist |

---

## Filesystem confinement (Landlock LSM)

Each service is confined to a declared set of filesystem paths and access rights
using **Linux Landlock** (kernel 5.13+, ABI v1). A compromised service cannot
read model weights, session history, or secrets belonging to another service even
if it calls `open(2)` -- the kernel silently denies access with `EACCES`.

### Why Landlock instead of AppArmor or SELinux

| Property | Landlock | AppArmor | SELinux |
|----------|----------|----------|---------|
| Policy source | TOML file in rootfs | Text profile loaded by kernel module | Binary policy (complex toolchain) |
| Kernel module required | No (built-in from 5.13+) | Yes (`apparmor_parser`) | Yes (`semanage`, `restorecon`) |
| Policy labelling | Path-based (no xattrs) | Path-based | Label-based (xattr on every file) |
| Unprivileged self-confinement | Yes -- process restricts itself | No -- requires root to load profiles | No |
| Fit for single-binary appliance | Excellent | Good | Poor (policy toolchain too heavy) |

Landlock requires no external tooling and no kernel module. Each service calls
`landlock_restrict_self(2)` (syscall 446) to install its own ruleset. The
restriction is inherited across fork and exec, so it applies to the entire
privilege-drop chain (`su -> sh -> service`).

### Profile format

Profiles live in `/etc/nura/landlock/<service>.toml`:

```toml
[[paths]]
path = "/data/sessions"
access = ["read_file", "write_file", "read_dir", "make_reg", "remove_file"]

[[paths]]
path = "/etc"
access = ["read_file", "read_dir"]
```

Available access rights (Landlock ABI v1):

| Name | Landlock right | Meaning |
|------|---------------|---------|
| `execute` | `LANDLOCK_ACCESS_FS_EXECUTE` | Execute a file |
| `write_file` | `LANDLOCK_ACCESS_FS_WRITE_FILE` | Write to an existing file |
| `read_file` | `LANDLOCK_ACCESS_FS_READ_FILE` | Read file contents |
| `read_dir` | `LANDLOCK_ACCESS_FS_READ_DIR` | List directory entries |
| `remove_dir` | `LANDLOCK_ACCESS_FS_REMOVE_DIR` | Unlink a directory |
| `remove_file` | `LANDLOCK_ACCESS_FS_REMOVE_FILE` | Unlink a file |
| `make_char` | `LANDLOCK_ACCESS_FS_MAKE_CHAR` | Create character device |
| `make_dir` | `LANDLOCK_ACCESS_FS_MAKE_DIR` | Create directory |
| `make_reg` | `LANDLOCK_ACCESS_FS_MAKE_REG` | Create regular file |
| `make_sock` | `LANDLOCK_ACCESS_FS_MAKE_SOCK` | Create UNIX domain socket |
| `make_fifo` | `LANDLOCK_ACCESS_FS_MAKE_FIFO` | Create named pipe |
| `make_block` | `LANDLOCK_ACCESS_FS_MAKE_BLOCK` | Create block device |
| `make_sym` | `LANDLOCK_ACCESS_FS_MAKE_SYM` | Create symbolic link |

### Per-service profile locations

| Service | Profile | Notable restrictions |
|---------|---------|---------------------|
| nura-agent | `/etc/nura/landlock/nura-agent.toml` | No access to `/data/models` or other service sockets |
| gateway | `/etc/nura/landlock/gateway.toml` | No access to `/data/sessions` or `/data/models` |
| llama-server | `/etc/nura/landlock/llama-server.toml` | Read-only `/data/models`; no `/data/sessions` or `/data/etc` |

All profiles include `/bin`, `/sbin`, and `/etc` (read-only) to accommodate the
`su -> sh` privilege-drop chain that runs before the service binary.

### Unit configuration

```toml
[landlock]
profile = "/etc/nura/landlock/nura-agent.toml"
```

### Kernel configuration

```
CONFIG_SECURITY=y
CONFIG_SECURITY_LANDLOCK=y
CONFIG_LSM="landlock"
```

`CONFIG_LSM="landlock"` makes Landlock the only active LSM. AppArmor and SELinux
are absent from the image, so this is the correct setting for the appliance.

### ABI version probing

At startup, `nura-manager seccomp-exec` probes the Landlock ABI version by calling
`landlock_create_ruleset(NULL, 0, LANDLOCK_CREATE_RULESET_VERSION)`. If the kernel
returns ABI < 1 (kernel older than 5.13), the restriction is skipped with a warning
log entry; the service starts unconfined. Production kernels built from
`kernel/configs/nuraos_x86_64_defconfig` always have ABI >= 1.

### Extending a profile

1. Start the service with `mode = "log"` in `[seccomp]` and observe denied paths
   via `dmesg | grep landlock` (kernel logs denied accesses at pr_debug level on
   some configs) or by testing the service under load.
2. Add the missing path with the required access rights to the profile TOML.
3. Restart the service to pick up the updated profile.

### Cross-service confinement boundary

| Service A wants to access... | Allowed? |
|------------------------------|----------|
| nura-agent reading `/data/models` | No -- not in nura-agent profile |
| gateway reading `/data/sessions` | No -- not in gateway profile |
| llama-server writing `/data/etc` | No -- not in llama-server profile |
| llama-server reading `/data/models` | Yes -- explicitly declared |
| nura-agent creating `/run/nura-agent.sock` | Yes -- `make_sock` on `/run` |
