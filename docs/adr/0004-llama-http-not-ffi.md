# ADR 0004: llama.cpp as an HTTP server, not an FFI dependency

**Status:** Accepted

## Context

The agent needs to call an inference backend. llama.cpp is the chosen local
model runtime. Two integration options:
- Link llama.cpp as a C library into the Rust agent via FFI
- Run llama.cpp as `llama-server` and call it over HTTP on loopback

## Decision

Run `llama-server` as a separate process on `127.0.0.1:8081`. The Rust agent
makes HTTP requests to it via the `LocalProvider`.

## Consequences

**Good:**
- The agent binary is pure Rust; no C FFI surface to maintain or audit.
- `llama-server` is independently restartable by the supervisor.
- The HTTP interface is standard OpenAI-compatible; swapping backends later
  requires only changing the base URL and model name.
- Debugging inference issues can be done by hitting the server directly.
- Build complexity is isolated to `scripts/build-llama.sh`.

**Bad:**
- Two processes instead of one; one extra HTTP hop per token.
- Context between turns crosses a process boundary (managed by the agent).
- llama-server startup time (model loading) adds to the boot-to-first-token
  latency (60-70 s for a 7B model on 2 vCPUs).
