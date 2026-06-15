# Security Policy

## Supported versions

NuraOS is pre-1.0. Only the `main` branch receives security fixes.

## Reporting a vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Report security issues privately to: arafath.yasser@gmail.com

Please include:
- A description of the vulnerability and its potential impact
- Steps to reproduce or a proof-of-concept (if safe to share)
- The version or commit SHA where the issue was observed

You will receive an acknowledgement within 72 hours. Fixes are coordinated
privately and disclosed after a patch is available.

## Scope

Vulnerabilities that are in scope:
- Unauthenticated access to the gateway API
- Token leakage through logs, metrics, or error responses
- Path traversal in the file-system tool
- Request body parsing bugs that cause memory exhaustion or panics
- Authentication bypass in the bearer-token middleware

Out of scope:
- Physical access to the host machine
- Vulnerabilities requiring a compromised QEMU host OS
- Lack of TLS (the gateway is loopback-only by default; TLS is a host concern)
- Theoretical timing attacks against the constant-time token comparison that
  require more requests than the rate limiter allows

## Security model summary

See [docs/security.md](docs/security.md) for the full threat model, default
configuration, and hardening guidance.

Key defaults:
- Gateway binds to `127.0.0.1:8080` (loopback only)
- No bearer token required by default (add one before LAN exposure)
- Per-IP rate limit: 1 req/s, burst 10
- Global concurrency cap: 4 simultaneous requests
- Bearer token comparison uses `crypto/subtle.ConstantTimeCompare`

## Verification

Run the security posture check from the repository root:

```sh
./scripts/security-check.sh
```

The CI pipeline also runs a secrets-scan job on every PR.
