# Suite: agent-core

Exercises the Rust agent core and the gateway HTTP API against a live
NuraOS instance. All cases run deterministically without a loaded language
model so the suite is green in CI environments that do not fetch the optional
model blob.

## Cases

| Case | Phase | Assertion |
| --- | --- | --- |
| `version` | 12 | `GET /version` returns `service=nura-gateway` and a non-empty version string |
| `status-ok` | 12 | `GET /status` returns 200 with an `agent` component |
| `no-secrets-leaked` | 26/27 | `/status` and `/config` bodies contain no API key patterns |
| `provider-default` | 11 | Agent status includes a non-empty provider field |
| `repl-provider` | 14 | Serial `:provider` produces provider-name output within 10 s |
| `repl-tools` | 14 | Serial `:tools` lists at least the built-in `echo` tool |
| `repl-clear` | 14 | Serial `:clear` produces a session-cleared confirmation |
| `log-structured` | 13 | Serial output contains structured log lines (compact or JSON) |

## Config-layer notes

The agent reads its configuration in ascending priority order:

1. Built-in Rust defaults (`Config::default()`) -- `provider.active = "local"`
2. `/etc/nura/agent.toml` -- system-wide overrides packed into the initramfs
3. `/data/etc/agent.toml` -- per-device persistent overrides
4. Environment variables -- `NURA_LOG_LEVEL`, `NURA_PORT`, `NURA_PROVIDER`

The `provider-default` case verifies layer 1 by checking the status endpoint.
Full layer testing (writing to `/data/etc/agent.toml` at runtime) requires
shell access to the guest and is deferred to a dedicated regression fixture.

## How to run

```sh
NURA_REPO_ROOT=/path/to/nuraos tests/run-suite agent-core
```

Reports are written to `tests/reports/agent-core/`.
