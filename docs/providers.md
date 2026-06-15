# NuraOS Provider Abstraction

## Overview

NuraOS uses a provider-agnostic abstraction layer so the agent loop is
completely decoupled from the underlying inference backend. Swapping from
the local llama.cpp server to a cloud API requires no changes to the agent
logic -- only a different `Provider` implementation is registered.

## Canonical IR

All conversation state flows through two provider-neutral types:

### Message

```
Message {
    role: Role,       // System | User | Assistant | Tool
    parts: Vec<ContentPart>,
}
```

`ContentPart` variants:

| Variant | Purpose |
|---------|---------|
| `Text { text }` | Plain text turn content |
| `ToolCallRequest { id, name, arguments }` | Assistant wants to call a tool |
| `ToolCallResult { call_id, output, error }` | Tool result returned to the model |

### StreamEvent

The `complete()` method yields an iterator of `StreamEvent`:

| Variant | Description |
|---------|-------------|
| `TokenDelta { text }` | Incremental text chunk (streaming) |
| `ToolCallDelta { id, name, arguments_chunk }` | Incremental tool-call being assembled |
| `Usage(Usage)` | Cumulative token counts |
| `Done { stop_reason }` | Turn finished: `EndOfTurn`, `MaxTokens`, `ToolCall`, `Cancel`, `Error` |
| `Error { message }` | Provider error; stream ends here |

### Usage

```
Usage {
    prompt_tokens: u32,
    completion_tokens: u32,
}
```

## Provider Trait

```rust
pub trait Provider: Send + Sync {
    fn name(&self) -> &str;
    fn capabilities(&self) -> Capabilities;
    fn complete(
        &self,
        messages: &[Message],
        params: &SamplingParams,
        cancel: &CancelToken,
    ) -> Box<dyn Iterator<Item = Result<StreamEvent>> + Send + '_>;
}
```

### Capabilities

```
Capabilities {
    streaming: bool,
    tool_calling: bool,
    system_messages: bool,
    max_context_tokens: u32,
}
```

The agent loop queries `capabilities()` once at startup to decide whether
to use streaming paths, tool-call dispatch, etc.

### SamplingParams

```
SamplingParams {
    temperature: f32,     // default 0.7
    top_p:       f32,     // default 0.95
    max_tokens:  u32,     // default 2048
    stop:        Vec<String>,
}
```

### CancelToken

`CancelToken` is a cheap `Arc<AtomicBool>` clone. Call `.cancel()` from any
thread to signal the provider. The provider must poll `.is_cancelled()`
between stream chunks and emit `Done { stop_reason: Cancel }` when set.

## Core Invariant (CI-enforced)

> **The agent loop (nura-core::agent, Phase 25+) must ONLY depend on the
> canonical IR types in `nura-core::provider` and the `Provider` trait.
> It must NEVER import a concrete provider type.**

Concrete providers (LocalProvider, AnthropicProvider, OpenAIProvider) live
in their own crates or modules that depend on `nura-core`, not the other
way around. This keeps the dependency graph acyclic and makes provider
substitution possible at compile time.

A test in each provider crate checks that it is not re-exported from
`nura-core`, enforcing this invariant automatically in CI.

## Provider Registry

`ProviderRegistry::from_config(cfg, secrets)` constructs all enabled providers
from the loaded `Config` and `Secrets`. The local provider is always registered.
Remote providers (`anthropic`, `openai`) are registered only when:

1. The binary was built with `--features remote-providers`, AND
2. The corresponding API key is present in secrets.

The registry exposes:
- `get(name)` -- look up a provider by name
- `default_provider()` -- the provider named by `provider.active` in config,
  falling back to the first registered provider
- `list_entries()` -- iterate `ProviderEntry` (name, tier, capabilities) for display
- `probe_local_reachability()` -- HTTP health check for local providers;
  remote providers are marked `Skipped` (no network call, no key use)

The agent exits at boot with code 2 if the registry is empty.

## Registered Providers

| Name | Tier | Opt-in | Backend |
|---|---|---|---|
| `local` | local | always | llama-server on 127.0.0.1:8081 |
| `anthropic` | cloud | `remote-providers` feature + key | Anthropic Messages API |
| `openai` | cloud | `remote-providers` feature + key | OpenAI Chat Completions API |
| `ollama` | local | `NURA_OLLAMA=1` | Ollama on 127.0.0.1:11434 |
| `lm-studio` | local | `NURA_LMSTUDIO=1` | LM Studio on 127.0.0.1:1234 |
| `custom` | cloud | `NURA_CUSTOM_ENDPOINT=http://...` | Any OpenAI-compatible endpoint |
| `llama-ffi` | local | `llama-ffi` Cargo feature | Direct llama.cpp FFI (skeleton) |

The active provider is selected by `provider.active` in `agent.toml`
(see [config.md](config.md)). When the named provider is not registered
(e.g. key missing), the agent falls back to `local` with a warning.

## Configuration Examples

### OpenAI

```toml
# /data/etc/agent.toml
[provider]
name = "openai"
model = "gpt-4o-mini"
base_url = "https://api.openai.com"
```

```toml
# /data/etc/secrets.toml
openai_api_key = "sk-..."
```

```sh
# Or via environment variable:
export OPENAI_API_KEY=sk-...
```

### vLLM (local OpenAI-compatible server)

```toml
[provider]
name = "openai"
model = "meta-llama/Llama-3.1-8B-Instruct"
base_url = "http://0.0.0.0:8000"
```

No API key required for a private vLLM instance.

### LiteLLM proxy / sovereign gateway

```toml
[provider]
name = "openai"
model = "anthropic/claude-haiku-4-5-20251001"
base_url = "http://litellm-proxy:4000"
```

```sh
export NURA_GATEWAY_TOKEN=your-litellm-key
```

### Anthropic direct

```toml
[provider]
name = "anthropic"
model = "claude-haiku-4-5-20251001"
base_url = "https://api.anthropic.com"  # default; omit for standard endpoint
```

```sh
export ANTHROPIC_API_KEY=sk-ant-...
```

### Ollama (sovereign, local)

```sh
export NURA_OLLAMA=1
export NURA_OLLAMA_MODEL=llama3   # optional; defaults to llama3
```

Ollama must be running on `127.0.0.1:11434`. No API key required.
Switch the active provider in `agent.toml`:

```toml
[provider]
active = "ollama"
```

### LM Studio (sovereign, local)

```sh
export NURA_LMSTUDIO=1
export NURA_LMSTUDIO_MODEL=local-model   # optional
```

LM Studio must be running on `127.0.0.1:1234`. No API key required.

### Custom OpenAI-compatible endpoint

```sh
export NURA_CUSTOM_ENDPOINT=http://my-inference-server:8000
export NURA_CUSTOM_MODEL=my-model-name   # optional
```

The `custom` provider uses the `openai_api_key` from secrets.toml as
an Authorization header if present. Omit the key for unauthenticated
endpoints (e.g. private vLLM or LiteLLM with no auth).

```toml
[provider]
active = "custom"
```

### Local (llama.cpp, offline)

```toml
[provider]
name = "local"
# base_url defaults to http://127.0.0.1:8081
```

No API key required. Run `bash scripts/fetch-model.sh` to download a model
then boot normally; the supervisor starts llama-server automatically.

## Adding a Provider

1. Implement `Provider` for your type in its own module.
2. Register it in the provider registry (Phase 20+).
3. Declare its `Capabilities` accurately.
4. Map your backend's streaming events into `StreamEvent`.
5. Poll `cancel.is_cancelled()` between every chunk.
6. Never import the type from inside `nura-core`.

## Resilience model

NuraOS monitors cloud provider reachability from the Go gateway layer and
implements a three-state circuit breaker per provider. When a provider
degrades, the gateway logs a deterministic fallback decision and routes
inference to the local llama-server instead.

### Health probes

The `services/internal/healthprobe` package fires a periodic HTTP GET against
each configured provider probe URL. A 2xx response is a success; any network
error or non-2xx response is a failure. Each probe runs in its own goroutine
and sends results to the provider health manager.

Default probe parameters:

| Parameter | Default |
|-----------|---------|
| Interval | 30 s |
| Timeout | 5 s |
| Fail threshold | 3 consecutive failures |
| Recovery threshold | 2 consecutive successes |
| Open duration (cooldown) | 30 s |

### Circuit breaker states

```
Closed --[N failures]--> Open --[cooldown elapsed]--> HalfOpen
  ^                                                        |
  +-----[M successes]----------------------------------+  |
                                                          |
                        Open <--[failure in HalfOpen]----+
```

| State | Requests allowed | Description |
|-------|-----------------|-------------|
| `closed` | yes | Normal; failures increment the counter |
| `open` | no | Tripped; all requests fall back to local |
| `half-open` | yes (probes only) | Recovery; enough successes close the circuit |

State transitions publish `provider.healthy` or `provider.degraded` events on
the event bus so operators can react via `nuractl events`.

### Fallback routing

`Manager.ShouldFallback(name)` returns `true` when the named provider's
circuit is Open. The agent router checks this before dispatching to a cloud
provider; a `true` result causes the request to be sent to the local
llama-server instead. The fallback decision is logged at `WARN` level with
the provider name and current circuit state.

### /status surface

`GET /status` includes one component per registered provider:

```json
{
  "name": "provider:anthropic",
  "status": "degraded",
  "detail": "circuit open (falling back to local)"
}
```

When the circuit is closed or half-open, `status` is `"ok"` and `detail`
shows the circuit state string.

### /metrics surface

Three metric families are emitted per registered provider:

```
# TYPE nura_provider_circuit_breaker_state gauge
nura_provider_circuit_breaker_state{provider="anthropic"} 2

# TYPE nura_provider_probe_success_total counter
nura_provider_probe_success_total{provider="anthropic"} 47

# TYPE nura_provider_probe_failure_total counter
nura_provider_probe_failure_total{provider="anthropic"} 3
```

Circuit state encoding: `0` = closed, `1` = half-open, `2` = open.

### Logging

Every probe failure is logged at `WARN` level:

```
WARN provider probe failed provider=anthropic circuit=open err="connection refused"
```

Fallback decisions are deterministic: the same provider state always produces
the same routing outcome with the same log line. No random jitter or silent
routing is applied.
