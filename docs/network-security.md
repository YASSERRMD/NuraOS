# Network Security

NuraOS enforces a default-deny network posture at the kernel packet level
using nftables, applied early in the boot sequence after DHCP completes.

## Firewall design

### Policy summary

| Direction | Default | Exceptions |
|---|---|---|
| Inbound | DROP | loopback, established/related, ICMP (rate-limited), gateway port |
| Forward | DROP | none (appliance is not a router) |
| Outbound | DROP | loopback, established/related, ICMP, DNS (53), DHCP (67), configured provider IPs |

### Ruleset location

The ruleset is generated and applied at boot by `/sbin/nura-firewall-apply`.
No static ruleset file is committed to disk -- the script builds an `nft -f`
input from `/etc/nura/firewall.conf` to allow runtime customization.

The table is named `inet nura` (covers IPv4 and IPv6).

## Configuration

Edit `/etc/nura/firewall.conf`:

```sh
# Gateway inbound port (must match GATEWAY_PORT env var).
GATEWAY_PORT=8080

# Space-separated outbound IP allowlist for provider endpoints.
# Leave empty for offline mode.
OUTBOUND_IPS="203.0.113.10 198.51.100.0/24"

# NTP server IP (outbound UDP 123).
NTP_IP="162.159.200.1"
```

To reload after editing:

```sh
nura-firewall-apply
```

## Boot sequence

```
/init: DHCP completes (outbound broadcast is unrestricted before this point)
  |
  v
/sbin/nura-firewall-apply
  |-- reads /etc/nura/firewall.conf
  |-- generates nftables config
  |-- nft -f /tmp/nura-nft-XXXXX.conf
  |-- logs result to console
  v
supervisor -> nura-manager (services start inside the firewall)
```

The firewall is applied **after** DHCP so that broadcast DHCP discover and
the subsequent DHCP offer are not blocked. Once DHCP assigns an address, the
ruleset drops all unmatched traffic.

## Inbound rules

1. `iif lo accept` -- loopback traffic is always trusted
2. `ct state established,related accept` -- return traffic for outbound connections
3. ICMP rate-limited at 10 pkt/s (prevents flood-based port scanning)
4. `tcp dport ${GATEWAY_PORT} accept` -- nura-gateway only

All other inbound TCP/UDP packets are logged with prefix `nura-inbound-drop:`
and dropped. Logs appear in the kernel ring buffer (`dmesg`).

### LAN-accessible gateway

When the gateway is configured with `GATEWAY_BIND_LAN=1` (env var), it binds
to `0.0.0.0`. The firewall gateway port rule still applies and controls which
source IPs can reach it. To restrict to a specific LAN subnet, replace the
gateway rule with:

```nft
ip saddr 192.168.1.0/24 tcp dport 8080 accept comment "gateway LAN-only"
```

## Egress allowlist

The default policy drops all unmatched outbound traffic. Only:

- Loopback (services communicating locally)
- Return traffic for established connections
- DNS queries (UDP/TCP port 53) -- required for name resolution
- DHCP discovery (UDP port 67) -- required for network renewal
- Configured provider IPs (`OUTBOUND_IPS`)
- NTP (`NTP_IP`, UDP port 123)

...are permitted.

### Offline mode

Set `OUTBOUND_IPS=""` and `NTP_IP=""` in `firewall.conf`. The system boots
and runs with no required external connectivity:

- DNS queries are allowed but return `NXDOMAIN` / timeout (no resolver)
- Provider calls from nura-agent fail with connection refused / EAGAIN
- All other services (gateway, llama-server) operate locally

No outbound connection is required for the NuraOS supervisor or inference
stack to start. The firewall is not bypassed at any point during boot.

## Kernel configuration

```
CONFIG_NETFILTER=y
CONFIG_NF_CONNTRACK=y
CONFIG_NF_TABLES=y
CONFIG_NF_TABLES_INET=y
CONFIG_NFT_CT=y
CONFIG_NFT_COUNTER=y
CONFIG_NFT_LOG=y
CONFIG_NFT_REJECT=y
CONFIG_NFT_REJECT_INET=y
```

`nft` (userspace CLI) must be present in the initramfs. The build script copies
it from the host if found. If absent, boot continues but rules are not applied.

## Security considerations

- The gateway is the only inbound surface. llama-server (8081) and nura-agent
  are loopback-only and cannot be reached from outside the VM.
- The event bus socket (`/run/nura-events.sock`) is a local Unix socket; it is
  not accessible over the network.
- Log prefixes `nura-inbound-drop:` and `nura-outbound-drop:` allow security
  monitoring by scraping `dmesg` or the kernel journal.
- Provider endpoint IPs should be reviewed regularly; dynamic IP assignment
  by providers may require updating `OUTBOUND_IPS` and re-running
  `nura-firewall-apply`.

---

## TLS configuration

All outbound HTTPS connections from Go services use `internal/tlsconfig` which:

- **Always verifies** the server's certificate chain -- `InsecureSkipVerify` is
  never set, including in fallback paths
- **Minimum TLS 1.2** -- TLS 1.0 and 1.1 are rejected at the client level
- **Explicit CA pool** -- loads the bundled or OS-provided CA pool before
  making any connection; a failed pool load is an error, never a silent bypass

### CA certificate bundle

The build script (`scripts/build-initramfs.sh`) copies the host's CA bundle into
the initramfs at `/etc/ssl/certs/ca-certificates.crt`.

Go's `x509.SystemCertPool()` reads this path on Linux. If the file is absent, Go
falls back to the compiled-in root set (Go 1.20+ always includes Mozilla's roots).

To override the bundle at runtime:

```sh
export NURA_CA_BUNDLE=/data/etc/custom-ca.crt
```

To update the bundle (as root):

```sh
# Replace with an updated Mozilla bundle:
wget -O /data/etc/ca-certificates.crt \
  https://curl.se/ca/cacert.pem
export NURA_CA_BUNDLE=/data/etc/ca-certificates.crt
# Restart services that use HTTPS forwarding.
```

### Certificate failure handling

A TLS handshake failure (expired cert, unknown CA, hostname mismatch) causes
the operation to fail with a clear Go error; there is no insecure retry:

```
journal forwarder: TLS setup failed; dropping record  url=https://... err=...
```

Services log TLS errors at Warning or Error level with the full error message;
they never silently fall back to HTTP or an unverified connection.

---

## DNS configuration

### resolv.conf management

`/init` writes `/etc/resolv.conf` only if DHCP did not provide one:

```sh
nameserver 10.0.2.3   # QEMU user-mode NAT resolver
nameserver 1.1.1.1    # Cloudflare public resolver (fallback)
```

DHCP-provided nameservers always take priority. The active nameservers are
logged at boot for diagnostic purposes:

```
[init] DNS: nameservers: 10.0.2.3 1.1.1.1
```

### Offline mode

With no DNS server reachable, name resolution fails. Go's `net.Dial` returns
`no such host` errors. Provider calls that require DNS fail immediately and are
not retried silently. The firewall still allows UDP port 53 outbound (queries
are permitted; timeouts are handled by the caller).
