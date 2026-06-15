# NuraOS Toolchain Versions

All build inputs are pinned here. Later scripts and CI read these values.
Update this file and the lock manifest together when upgrading a component.

## Linux Kernel

| Field        | Value                                    |
|--------------|------------------------------------------|
| Repository   | https://git.kernel.org/pub/scm/linux/kernel/git/stable/linux.git |
| Tag          | v6.6.87                                  |
| Branch       | linux-6.6.y (LTS)                        |
| Tarball URL  | https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-6.6.87.tar.xz |
| SHA256       | (recorded in kernel/PINNED.md after fetch) |

Rationale: 6.6 is an active LTS line with a maintenance window extending
through December 2026, providing a stable foundation without chasing mainline
churn. See [kernel.md](kernel.md) for the full rationale.

## musl libc

| Field     | Value                                    |
|-----------|------------------------------------------|
| Version   | 1.2.5                                    |
| Source    | https://musl.libc.org/releases/musl-1.2.5.tar.gz |
| SHA256    | (recorded by fetch-musl.sh at download time) |

Used to build all userland binaries as fully static executables with no
dependency on a dynamic linker.

## BusyBox

| Field     | Value                                    |
|-----------|------------------------------------------|
| Version   | 1.37.0                                   |
| Source    | https://busybox.net/downloads/busybox-1.37.0.tar.bz2 |
| SHA256    | (recorded by fetch-busybox.sh at download time) |

## Cross / Native GCC

NuraOS targets x86-64 and compiles on x86-64, so the host GCC serves as the
native compiler. musl-gcc (a wrapper around the host GCC) is used for
static-musl builds.

| Field       | Value                       |
|-------------|-----------------------------|
| Host GCC    | >= 12.0 (any recent version) |
| musl-gcc    | provided by musl-cross-make or distro musl-tools |
| Target      | x86_64-linux-musl           |

## Rust Toolchain

| Field    | Value                              |
|----------|------------------------------------|
| Channel  | stable                             |
| Version  | 1.87.0                             |
| Target   | x86_64-unknown-linux-musl          |
| Pin file | agent/rust-toolchain.toml          |

## Go

| Field   | Value   |
|---------|---------|
| Version | 1.23.4  |
| Pin     | services/go.mod `go` directive     |

## llama.cpp

| Field  | Value                                                        |
|--------|--------------------------------------------------------------|
| Repo   | https://github.com/ggerganov/llama.cpp                       |
| SHA    | b5903 (tag: b5903)                                           |
| Path   | third_party/llama.cpp                                        |

The SHA is recorded in third_party/llama.cpp-SHA when the submodule is pinned.

## QEMU

| Field   | Value                          |
|---------|--------------------------------|
| Version | >= 8.2.0                       |
| Binary  | qemu-system-x86_64             |
| Purpose | primary test and development target |

## Lock manifest

A single machine-readable lock is kept at [scripts/VERSIONS.env](../scripts/VERSIONS.env).
Scripts source this file for version strings rather than hard-coding them.
