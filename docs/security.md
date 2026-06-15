# NuraOS Security Model

NuraOS is designed as a local-first appliance running inside a QEMU VM on a
trusted machine. This document describes the threat model, the protections in
place, and guidance for safe LAN exposure.

## Threat model

**In scope (protected)**:
- Unauthorised access from the LAN when the gateway is exposed.
- Request floods and resource exhaustion from misbehaving clients.
- MIME-type confusion and clickjacking via browser-level attacks.
- Information leakage of provider API keys through logs or metrics.

**Out of scope (not protected at this layer)**:
- Physical access to the host machine.
- A compromised QEMU host OS.
- Traffic interception in transit (no TLS; see below).
- A malicious model payload loaded from `/data/models/`.

## Default configuration

By default the gateway binds to `127.0.0.1:8080` and is accessible only from
the host running QEMU. No bearer token is required. This is the safe default
for single-user development.

```
Host: curl http://localhost:18080/healthz
```

## Enabling LAN exposure (opt-in)

To expose the gateway on all interfaces (e.g. a home server), set:

```sh
GATEWAY_BIND_LAN=1
```

**Before doing so, configure a bearer token** (see below), or any device on
the LAN can query your AI appliance and read files accessible to the agent.

## Bearer-token authentication

Create `/data/etc/secrets.toml` inside the guest:

```sh
mkdir -p /data/etc
chmod 700 /data/etc
cat > /data/etc/secrets.toml <<'EOF'
gateway_token = "replace-with-a-long-random-string"
EOF
chmod 600 /data/etc/secrets.toml
```

Generate a strong token:
```sh
head -c 32 /dev/urandom | base64 | tr -d '=/+'
```

When `gateway_token` is set, all endpoints except `GET /healthz` require:
```
Authorization: Bearer <your-token>
```

Unauthenticated requests receive `401 Unauthorized`.

Example curl with auth:
```sh
curl -H "Authorization: Bearer <token>" http://host:18080/tools
```

## Rate limiting and concurrency

The gateway enforces two independent limits to prevent resource exhaustion:

| Limit | Default | Scope |
|-------|---------|-------|
| Requests per second | 1 req/s (burst 10) | Per client IP |
| Concurrent requests | 4 | Global (all clients) |

Requests exceeding either limit receive `429 Too Many Requests` with a
`Retry-After: 1` header. The `/healthz` endpoint is exempt from both limits
so monitoring tools always get a response.

## Security response headers

All responses include:

| Header | Value | Purpose |
|--------|-------|---------|
| `X-Content-Type-Options` | `nosniff` | Prevent MIME sniffing |
| `X-Frame-Options` | `DENY` | Prevent clickjacking |
| `Content-Security-Policy` | `default-src 'none'` | No resource loading |

## Transport security

The gateway does NOT implement TLS. For LAN exposure with encryption:

1. Run a TLS-terminating reverse proxy (nginx, caddy) on the host.
2. Forward decrypted traffic to the QEMU port forward (e.g. `localhost:18080`).
3. Clients connect to the proxy over HTTPS.

Example nginx snippet:
```nginx
server {
    listen 443 ssl;
    ssl_certificate     /etc/ssl/nura.crt;
    ssl_certificate_key /etc/ssl/nura.key;
    location / {
        proxy_pass http://127.0.0.1:18080;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

## Secrets management

- Provider API keys live in `/data/etc/secrets.toml` with mode `0600`.
- Keys are loaded at agent startup; they are never logged.
- To rotate a key: update the file and restart the agent
  (`kill -HUP <nura-agent-pid>` or restart via supervisor).
- Keys must not be committed to git; the `/data/` subtree is not tracked.

## Attack surface summary

| Attack | Mitigation |
|--------|-----------|
| Unauth API calls | Bearer token; disabled by default when no token set |
| DDoS / request flood | Per-IP rate limit + global concurrency cap |
| MIME confusion | `X-Content-Type-Options: nosniff` |
| Clickjacking | `X-Frame-Options: DENY` |
| Large request bodies | 64 KiB cap on POST /chat |
| Slow-client attacks | `ReadTimeout: 10 s` on the HTTP server |
| Path traversal in tools | `fs.read` rejects `..` components lexically |
