# Host Setup

This document describes how to set up a development machine to build NuraOS.
Run `scripts/check-host.sh` to verify prerequisites after following these steps.

## Required tools

| Tool               | Minimum version | Purpose                                 |
|--------------------|-----------------|------------------------------------------|
| make               | 4.0             | Kernel and userland builds               |
| gcc                | 12.0            | Native compilation                       |
| bc                 | any             | Kernel Makefile arithmetic               |
| flex               | any             | Kernel lexer generation                  |
| bison              | any             | Kernel parser generation                 |
| libelf / pahole    | any             | Kernel BTF / debug info                  |
| openssl            | 3.0             | Kernel certificate and signature tools   |
| xz                 | any             | Kernel tarball decompression             |
| cpio               | any             | Initramfs assembly                       |
| git                | 2.30            | Source fetching, submodule management    |
| curl               | any             | Tarball downloads                        |
| qemu-system-x86_64 | 8.2.0           | Primary test target                      |
| rustup / rustc     | 1.87.0          | Agent core compilation                   |
| go                 | 1.23.4          | Gateway and services compilation         |

## Debian / Ubuntu

```sh
sudo apt-get update
sudo apt-get install -y \
    build-essential bc flex bison libelf-dev libssl-dev \
    xz-utils cpio git curl \
    qemu-system-x86 \
    pahole
```

Install Rust via rustup (do not use the distro package):
```sh
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
source "$HOME/.cargo/env"
rustup target add x86_64-unknown-linux-musl
```

Install Go from the official releases (do not use the distro package):
```sh
GO_VER="1.23.4"
curl -LO "https://go.dev/dl/go${GO_VER}.linux-amd64.tar.gz"
sudo tar -C /usr/local -xzf "go${GO_VER}.linux-amd64.tar.gz"
export PATH="$PATH:/usr/local/go/bin"
```

## Fedora / RHEL

```sh
sudo dnf install -y \
    make gcc bc flex bison elfutils-libelf-devel openssl-devel \
    xz cpio git curl \
    qemu-system-x86 \
    pahole
```

Follow the same rustup and Go steps as above.

## macOS (cross-compile support only)

macOS cannot build a Linux kernel natively. Use a Linux VM or Docker container.

For the Go gateway and agent (with a Linux Docker container):
```sh
brew install go rustup-init qemu
rustup-init
```

## Verify

After installing all tools, run:

```sh
./scripts/check-host.sh
```

All lines should show `[PASS]`. If any show `[FAIL]`, install the missing tool
and re-run the check before building.

## musl toolchain

The musl libc cross toolchain is fetched and built by `scripts/fetch-musl.sh`
(Phase 05). You do not need to install musl manually; the script handles it.

## Pinned versions

See [docs/toolchain.md](toolchain.md) for the exact version pins used by all
build scripts.
