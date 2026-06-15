# ADR 0003: Rust for the agent core; Go for the HTTP gateway

**Status:** Accepted

## Context

The AI agent needs to: manage context windows, execute tool calls, route to
inference providers, and record provenance. The HTTP gateway needs to: proxy
SSE streams, enforce auth, apply rate limiting, and emit Prometheus metrics.

These two roles have different tradeoffs. Options considered:
- Single Go binary for everything
- Single Rust binary for everything
- Rust agent + Go gateway (split)

## Decision

Rust for `nura-agent` (the agent core) and Go for `nura-gateway` (the HTTP
gateway). They communicate over a Unix domain socket using HTTP/1.1.

## Reasons for Rust in the agent

- Memory safety without a GC is important for a long-running inference loop.
- `async`/`await` in Rust (`tokio`) handles concurrent tool calls cleanly.
- The `x86_64-unknown-linux-musl` target produces a small, fully static binary.
- Provider trait abstraction is idiomatic in Rust (trait objects).

## Reasons for Go in the gateway

- The standard library has excellent HTTP primitives (`net/http`, SSE).
- Compilation is fast; the binary is small with `CGO_ENABLED=0`.
- `sync/atomic` and `sync.Pool` are straightforward for metrics and buffer reuse.
- Adding middleware layers is easy with `http.Handler` chaining.

## Consequences

**Good:**
- Each component is written in the language best suited to its role.
- The Unix socket boundary makes components independently restartable.
- Testing each component independently is straightforward.

**Bad:**
- Two languages mean two toolchains, two CI build steps, two test runners.
- The Unix socket adds one IPC hop per request (< 1 ms in practice).
