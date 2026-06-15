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

## Validation

```sh
nura-agent doctor
```

Doctor checks config, secrets redaction, filesystem layout, and provider
availability. All values are shown with secrets replaced by `[REDACTED]`.
