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

## Registered Providers

| Name | Phase | Backend |
|------|-------|---------|
| `stub` | 15 (current) | Echo stub, tests only |
| `local` | 17 | llama-server over 127.0.0.1 |
| `anthropic` | 18 | Anthropic Messages API |
| `openai` | 19 | OpenAI Chat Completions API |

The active provider is selected by `routing_policy` in `agent.toml`
(see [config.md](config.md)).

## Adding a Provider

1. Implement `Provider` for your type in its own module.
2. Register it in the provider registry (Phase 20+).
3. Declare its `Capabilities` accurately.
4. Map your backend's streaming events into `StreamEvent`.
5. Poll `cancel.is_cancelled()` between every chunk.
6. Never import the type from inside `nura-core`.
