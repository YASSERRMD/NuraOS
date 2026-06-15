# NuraOS Documentation Index

This index links the complete NuraOS documentation corpus. Sections
correspond to the phase packs (core phases 00-55, extended phases 56-105).

---

## Getting started

| Document | Description |
|----------|-------------|
| [handbook.md](handbook.md) | Operator handbook with runbooks for the full lifecycle |
| [host-setup.md](host-setup.md) | Host prerequisites and QEMU setup |
| [boot-chain.md](boot-chain.md) | Boot sequence and UEFI/GRUB configuration |
| [boot.md](boot.md) | Boot process details and kernel parameters |

---

## Architecture

| Document | Description |
|----------|-------------|
| [architecture.md](architecture.md) | System overview and component diagram |
| [init.md](init.md) | Init system and service manager design |
| [services.md](services.md) | Service unit model and lifecycle |
| [filesystem.md](filesystem.md) | Filesystem layout (/data, /boot, /run) |
| [userland.md](userland.md) | musl libc, BusyBox, and rootfs construction |
| [toolchain.md](toolchain.md) | Cross-compilation and build system |

---

## Networking

| Document | Description |
|----------|-------------|
| [network.md](network.md) | Networking stack, interface configuration |
| [network-security.md](network-security.md) | Firewall rules, iptables/nftables |
| [api.md](api.md) | HTTP gateway API reference (all endpoints) |

---

## Inference and AI

| Document | Description |
|----------|-------------|
| [inference.md](inference.md) | Model lifecycle, lazy load, idle timeout, warm pool |
| [providers.md](providers.md) | Provider abstraction, circuit breaker, resilience |
| [persona.md](persona.md) | System persona and prompt design |
| [tools.md](tools.md) | Agent tool framework and allowlist |

---

## Security

| Document | Description |
|----------|-------------|
| [security.md](security.md) | OS-level sandbox (Landlock, seccomp, caps) |
| [isolation.md](isolation.md) | Namespace isolation and cgroup resource limits |
| [provenance.md](provenance.md) | Request provenance and audit trail |
| [identity.md](identity.md) | Machine identity and hostname persistence |

---

## Storage and updates

| Document | Description |
|----------|-------------|
| [storage.md](storage.md) | /data layout, durability, fsck |
| [update.md](update.md) | A/B firmware update mechanism |
| [updates.md](updates.md) | Update signing, delta updates, rollback |
| [packages.md](packages.md) | Package manager (.nupkg format) |

---

## Observability

| Document | Description |
|----------|-------------|
| [observability.md](observability.md) | Prometheus metrics, Grafana dashboard, alerting |
| [logging.md](logging.md) | Journal, log rotation, structured logging |
| [events.md](events.md) | System event bus (types, sources, subscribers) |
| [telemetry.md](telemetry.md) | Privacy-preserving local telemetry |
| [perf.md](perf.md) | Performance profiling and benchmarks |

---

## Operations

| Document | Description |
|----------|-------------|
| [operating.md](operating.md) | Service management, self-test, crash diagnostics, backup/restore |
| [resilience.md](resilience.md) | Watchdog, circuit breaker, crash capture |
| [recovery.md](recovery.md) | Recovery mode and emergency access |
| [config.md](config.md) | Configuration schema, drift detection, history, rollback |
| [locale.md](locale.md) | UTF-8 enforcement, locale configuration, RTL text handling |

---

## Hardware

| Document | Description |
|----------|-------------|
| [kernel.md](kernel.md) | Kernel configuration and module selection |
| [devices.md](devices.md) | Hardware device support (virtio, serial, watchdog) |
| [power.md](power.md) | Power management and ACPI |
| [time.md](time.md) | Time synchronisation (NTP, RTC, virtio-clock) |

---

## Other

| Document | Description |
|----------|-------------|
| [repl.md](repl.md) | Serial REPL interface |
| [resources.md](resources.md) | cgroup v2 resource governance |
| [boards.md](boards.md) | Board-specific porting notes |
| [adr/](adr/) | Architecture Decision Records |
