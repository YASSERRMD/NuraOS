# ADR 001: Harness language: Go

## Status

Accepted

## Context

The NuraOS test harness must:
- Launch QEMU and manage its full lifecycle (boot, poll, kill, cleanup).
- Connect to the guest serial console via a UNIX socket for readiness
  detection and REPL interaction.
- Send HTTP requests to the guest gateway.
- Emit JUnit XML (for CI test annotations) and structured JSON reports.
- Compile to a single binary invoked as `run-suite <name>` from shell
  scripts and GitHub Actions steps.

Two primary candidates were evaluated: Rust (the agent core language) and
Go (the services/gateway language).

## Decision

Go was chosen.

Rationale:
1. **Stdlib completeness.** `os/exec`, `net`, `net/http`, `encoding/xml`,
   and `encoding/json` cover all harness needs without any external deps.
   The `go.sum` file is therefore empty and not required.
2. **Team familiarity.** The gateway code under test is also Go, so the
   same mental model applies to the harness and the system under test.
3. **Concurrency model.** A goroutine draining the serial socket into a
   buffer is idiomatic and straightforward in Go.
4. **Single binary.** `go build ./cmd/run-suite` produces a static binary
   with no dynamic linking requirements; easy to invoke in CI containers.
5. **Faster iteration.** Compile-link cycle is faster than Rust for a
   test-only binary where runtime performance is not the goal.

Rust would also have been suitable. The decision does not imply Rust is
inferior for this task; it simply avoids adding a cross-language cognitive
boundary between the harness and the HTTP service it exercises.

## Consequences

- `/tests` has its own `go.mod` (module `github.com/yasserrmd/nuraos/tests`).
- No external Go dependencies; only the standard library is used.
- The harness binary is built with `cd tests && go build ./cmd/run-suite`.
- The harness and agent code are separate modules; they do not share a
  workspace root `go.mod`.
