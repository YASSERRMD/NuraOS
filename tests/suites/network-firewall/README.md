# Suite T13 — network-firewall

Verifies that NuraOS enforces a secure-by-default network posture: loopback-only
gateway binding, presence of the net.status tool, and the firewall configuration
file in the repository.

## Cases

| Case | Source | Pass condition |
|------|--------|----------------|
| `gateway-loopback-only` | GET /config → `bind` field | Value is `127.0.0.1`; **skip** if field absent; **fail** if `0.0.0.0` |
| `net-status-tool` | GET /tools | Response contains `net.status` |
| `healthz-local-reachable` | GET /healthz | 200 (proves loopback port-forward is open) |
| `firewall-conf-exists` | `rootfs/etc/nura/firewall.conf` | File present in repository |

## Running

```
go run ./cmd/run-suite -- network-firewall
```
