# NuraOS Configuration

## Config precedence (lowest to highest)

1. Built-in defaults (compiled into nura-agent)
2. `/etc/nura/agent.toml` (system-wide, read-only rootfs)
3. `/data/etc/agent.toml` (per-device, writable /data)
4. Environment variables

Each layer overrides the previous. The `/data` layer is the primary operator
configuration point. Environment variables are useful for container or CI use.

## Schema

### [server]

| Key           | Type   | Default     | Description                       |
|---------------|--------|-------------|-----------------------------------|
| bind          | string | "127.0.0.1" | Bind address for the HTTP gateway |
| port          | u16    | 8080        | HTTP API port                     |
| metrics_port  | u16    | 9090        | Prometheus metrics port           |

### [provider]

| Key            | Type   | Default                         | Description                    |
|----------------|--------|---------------------------------|--------------------------------|
| active         | string | "local"                         | Active provider name           |
| routing        | enum   | "local_first"                   | Routing policy                 |
| model_manifest | path   | /data/models/model.json         | Local model manifest           |
| tool_allowlist | path   | /data/etc/tool_allowlist.toml   | Allowlisted tool names         |

Routing policy values: `local_first`, `remote_first`, `local_only`.

### [timeouts]

| Key                    | Type | Default | Description                       |
|------------------------|------|---------|-----------------------------------|
| turn_secs              | u64  | 120     | Maximum wall-clock time per turn  |
| tool_call_secs         | u64  | 30      | Per-tool-call timeout             |
| provider_connect_secs  | u64  | 10      | Provider connection timeout       |

### [token_budget]

| Key                 | Type | Default | Description                          |
|---------------------|------|---------|--------------------------------------|
| max_context_tokens  | u32  | 4096    | Maximum tokens in the context window |
| max_output_tokens   | u32  | 1024    | Maximum tokens in one completion     |
| max_tool_iterations | u32  | 10      | Maximum tool-call rounds per turn    |

### [log_level]

String value: `trace`, `debug`, `info`, `warn`, or `error`. Default: `info`.

## Environment variable overrides

| Variable            | Overrides               |
|---------------------|-------------------------|
| NURA_LOG_LEVEL      | log_level               |
| NURA_PORT           | server.port             |
| NURA_PROVIDER       | provider.active         |

## Secrets

Secrets are read from `/data/etc/secrets.toml` or environment variables.
They are **never** written to logs, metrics, or crash dumps.

### /data/etc/secrets.toml format

```toml
anthropic_api_key = "sk-ant-..."
openai_api_key    = "sk-..."
gateway_token     = "your-bearer-token"
```

### Environment variable overrides for secrets

| Variable             | Secret              |
|----------------------|---------------------|
| ANTHROPIC_API_KEY    | anthropic_api_key   |
| OPENAI_API_KEY       | openai_api_key      |
| NURA_GATEWAY_TOKEN   | gateway_token       |

**Permissions:** `/data/etc/secrets.toml` must be mode 0600 (owner-read only).
The agent checks permissions at startup and refuses to start if the file is
world-readable (enforced in Phase 33).

## Example /data/etc/agent.toml

```toml
[server]
bind = "127.0.0.1"
port = 8080

[provider]
active  = "local"
routing = "local_first"

[timeouts]
turn_secs = 120

[token_budget]
max_context_tokens  = 4096
max_output_tokens   = 1024
max_tool_iterations = 10

log_level = "info"
```

## Interactive configuration

Run the configure helper after first boot or to change provider:

```sh
./scripts/configure.sh
```

The script prompts for:
- Provider choice (`local`, `anthropic`, `openai`)
- API key (if cloud provider selected)
- Optional gateway bearer token

For automated (CI/scripted) setup pass `--non-interactive` and set the relevant
environment variables (`NURA_PROVIDER`, `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
`NURA_GATEWAY_TOKEN`).

## GET /config endpoint

The gateway exposes the effective configuration (no secrets) at `GET /config`:

```json
{
  "gateway": {
    "version": "dev",
    "port": "8080",
    "bind": "127.0.0.1",
    "auth_enabled": false,
    "rate_rps": 1.0,
    "rate_burst": 10,
    "max_concurrent": 4,
    "pprof_enabled": false
  },
  "agent": {
    "socket": "/run/nura-agent.sock"
  }
}
```

This endpoint is subject to bearer auth when a token is configured.
Useful for debugging; safe to expose to operators.

## Provider switching at runtime

Pass `"provider"` in the chat request body to override the default for that
turn:

```json
{
  "messages": [{"role": "user", "content": "Hello"}],
  "provider": "anthropic"
}
```

Valid provider names: `local`, `anthropic`, `openai`. The agent ignores unknown
values and falls back to the configured default.

## Validation

```sh
nura-agent doctor
```

Doctor checks config, secrets redaction, filesystem layout, and provider
availability. All values are shown with secrets replaced by `[REDACTED]`.

---

## Canonical config snapshot (configmgr)

Phase 97 introduced a structured, versioned config store at
`/data/config/nura.json` managed by the `configmgr` package. This is separate
from the TOML-based agent config above and covers the full system: agent
parameters, gateway settings, firewall rules, and static routing.

### JSON schema

```json
{
  "version": 3,
  "agent": {
    "model_path": "/data/models/qwen2.5-3b.gguf",
    "context_len": 4096,
    "threads": 4
  },
  "gateway": {
    "port": 8080,
    "bind_lan": false,
    "rate_rps": 10
  },
  "firewall": {
    "rules": [
      { "action": "allow", "proto": "tcp", "port": 8080, "src": "127.0.0.1/8" },
      { "action": "deny",  "proto": "any",  "port": 0 }
    ]
  },
  "routing": {
    "default_gateway": "",
    "static_routes": {}
  }
}
```

### Atomic apply

`configmgr.Store.Apply` validates the config, writes to a temp file, then
calls `rename(2)` atomically. If validation fails, the live file is untouched.

### Drift detection

`Store.Diff(running)` compares the disk snapshot to a live config struct and
returns a `DriftReport` listing every differing field with its snapshot and
running values.

### Version history and rollback

Every successful apply appends a JSON line to `/data/config/history.jsonl`.
Up to 50 entries are retained. `Store.RollbackTo(version)` re-validates and
atomically applies a previous snapshot, recording the rollback as a new entry.
