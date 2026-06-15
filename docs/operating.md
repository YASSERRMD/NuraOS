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

## Build

```sh
scripts/build-nuractl.sh      # builds rootfs/staging/sbin/nuractl
scripts/build-initramfs.sh    # includes nuractl in the initramfs
```
