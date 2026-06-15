# ADR 0001: Build kernel directly from kernel.org tarball

**Status:** Accepted

## Context

NuraOS needs a Linux kernel. Options include:
- Download a pre-built kernel from a distribution
- Use Buildroot to manage the kernel and userland together
- Fetch and build the kernel directly from kernel.org

## Decision

Fetch the kernel source tarball directly from kernel.org (SHA-256 verified),
apply no patches, and build from a hand-maintained `defconfig` fragment in
`kernel/configs/nuraos_x86_64_defconfig`.

## Consequences

**Good:**
- Full control over the kernel configuration with no abstraction layer.
- No Buildroot dependency to debug when build system behavior changes.
- The config file is small (100-200 lines) and self-documenting.
- Version pinning is trivial: one line in `VERSIONS.env`.

**Bad:**
- We maintain the config manually; Buildroot would auto-select options.
- Fetching the tarball adds 100-200 MB to the first build.
- No automatic security patch application (we must track CVEs).
