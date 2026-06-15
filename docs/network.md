# NuraOS Network Model

## Overview

NuraOS uses QEMU user-mode networking. The guest gets an IP via DHCP from
the internal QEMU DHCP server (10.0.2.0/24 subnet by default). The host
reaches the guest through explicit port forwards configured in `run-qemu.sh`.

## Guest network setup (boot sequence)

1. `/init` brings up loopback (`lo`).
2. `/init` brings up `eth0` (the virtio-net-pci device).
3. `udhcpc` runs with `/etc/udhcpc/default.script` to:
   - Assign the leased IP to `eth0`.
   - Add the default route via the QEMU gateway.
   - Write `/etc/resolv.conf` with the QEMU-provided DNS servers.
4. If DHCP fails, `/init` prints a warning and continues without network.
   All local functionality (serial REPL, local inference) remains available.

## QEMU user-mode networking

In user-mode networking the guest cannot be reached directly from the host.
The host accesses the guest only through port forwards.

Default QEMU network in NuraOS:
```
Host         Guest
127.0.0.1    10.0.2.15  (guest IP, QEMU default)
gateway      10.0.2.2   (QEMU host-side gateway)
DNS          10.0.2.3   (QEMU internal DNS forwarder)
```

## Host port forwards

| Host port | Guest port | Service                    |
|-----------|------------|----------------------------|
| 8080      | 8080       | HTTP API (Go gateway)       |
| 9090      | 9090       | Metrics endpoint            |

Customize ports with `run-qemu.sh --port-api N --port-metrics N`.

## curl from the host

```sh
# Check agent health (after Phase 28)
curl http://localhost:8080/healthz

# Send a chat request (after Phase 29)
curl -X POST http://localhost:8080/chat \
     -H "Content-Type: application/json" \
     -d '{"messages": [{"role": "user", "content": "hello"}]}'

# Get metrics (after Phase 31)
curl http://localhost:9090/metrics
```

## Network self-test (optional)

Pass `nura.nettest=1` on the kernel command line to run a gateway ping check
during boot. This is off by default so offline boots complete cleanly.

```sh
# Boot with network self-test enabled
./scripts/run-qemu.sh --kernel image/out/bzImage \
    # (edit KCMDLINE in run-qemu.sh to add nura.nettest=1)
```

## Offline boot

The default boot path works with no network:
- Local inference via llama.cpp does not require network.
- The serial REPL is available over ttyS0 regardless.
- DHCP failure produces a warning but does not abort boot.
- Remote provider calls are only made when explicitly configured and attempted.

## LAN exposure (opt-in)

By default the gateway binds to loopback (127.0.0.1) for security. To expose
it on the LAN you must:
1. Enable LAN bind in `agent.toml`.
2. Change the QEMU netdev from user-mode to a bridge or tap device.
3. Configure a bearer token for authentication.

See [docs/security.md](security.md) for the threat model and guidance.
