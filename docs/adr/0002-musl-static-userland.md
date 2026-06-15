# ADR 0002: musl libc and static binaries for all userland

**Status:** Accepted

## Context

Every binary in the initramfs must run without a dynamic linker. Options:
- glibc with static linking (works but glibc is very difficult to static-link cleanly)
- musl libc with static linking
- glibc with dynamic linking (requires `/lib` in the initramfs)

## Decision

Use musl libc (built from source, version pinned in `VERSIONS.env`) as the
sole C library. All userland binaries are statically linked against musl.
The Go gateway uses `CGO_ENABLED=0` (no C dependency). The Rust agent targets
`x86_64-unknown-linux-musl`.

## Consequences

**Good:**
- No dynamic linker or shared library tree needed in the initramfs.
- Single-file binaries are trivial to place in the initramfs.
- musl is MIT-licensed, well-audited, and small.
- Rust and Go both have excellent musl support.

**Bad:**
- Some C programs assume glibc extensions and require patches.
- musl's name resolver does not support all `/etc/nsswitch.conf` options.
- Debugging symbols in static binaries are larger.
