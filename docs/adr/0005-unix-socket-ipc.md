# ADR 0005: Unix domain socket for gateway-to-agent IPC

**Status:** Accepted

## Context

The Go gateway needs to communicate with the Rust agent. Options:
- TCP loopback (127.0.0.1:PORT)
- Unix domain socket
- Shared memory or pipes
- gRPC / protocol buffers

## Decision

Use a Unix domain socket at `/run/nura-agent.sock` with plain HTTP/1.1.

## Consequences

**Good:**
- No port number to allocate or conflict with.
- The socket file acts as a presence indicator for the supervisor's health check.
- Permissions on `/run/nura-agent.sock` can restrict access without firewall rules.
- HTTP/1.1 reuses the existing `net/http` and `hyper` tooling; no new protocol.
- Latency is lower than TCP loopback (no TCP handshake overhead).

**Bad:**
- Socket paths on macOS have a 104-character limit; tests must use short paths.
- The socket file must be cleaned up on restart (handled by the agent).
- Cross-host access is not possible (intentional: the gateway sits on the same host).
