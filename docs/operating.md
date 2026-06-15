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

## Build

```sh
scripts/build-nuractl.sh      # builds rootfs/staging/sbin/nuractl
scripts/build-initramfs.sh    # includes nuractl in the initramfs
```
