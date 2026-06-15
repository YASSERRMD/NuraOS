# NuraOS Built-in Tools

Tools extend the agent with structured access to system state. Each tool has
a JSON Schema for its arguments and returns a JSON object. The agent validates
arguments against the schema before execution and rejects malformed calls.

All tools in this document are read-only: they never modify filesystem state,
network configuration, or running processes.

---

## system.info

Returns a snapshot of host system information.

**Arguments:** none

**Returns:**

| Field | Type | Description |
|-------|------|-------------|
| `hostname` | string | Kernel hostname from `/proc/sys/kernel/hostname` |
| `os_version` | string | Kernel version string from `/proc/version` |
| `uptime_seconds` | float | Seconds since boot from `/proc/uptime` |
| `memory_total_kb` | integer | Total RAM (MemTotal from `/proc/meminfo`) |
| `memory_free_kb` | integer | Unallocated RAM (MemFree) |
| `memory_available_kb` | integer | Reclaimable RAM (MemAvailable) |
| `load_avg_1m` | float | 1-minute load average from `/proc/loadavg` |
| `load_avg_5m` | float | 5-minute load average |
| `load_avg_15m` | float | 15-minute load average |

**Example request:**
```json
{ "name": "system.info", "arguments": {} }
```

**Example response:**
```json
{
  "hostname": "nuraos",
  "os_version": "Linux version 6.12.0",
  "uptime_seconds": 3742.5,
  "memory_total_kb": 2048000,
  "memory_free_kb": 1024000,
  "memory_available_kb": 1800000,
  "load_avg_1m": 0.10,
  "load_avg_5m": 0.05,
  "load_avg_15m": 0.01
}
```

---

## fs.read

Read a file under `/data/`. Path traversal (`..`) and paths outside `/data/`
are rejected. Response is truncated to `max_bytes` when the file is larger.

**Arguments:**

| Field | Required | Type | Description |
|-------|----------|------|-------------|
| `path` | yes | string | Absolute path. Must start with `/data/`. |
| `max_bytes` | no | integer | Bytes to read. Default 65536; max 1048576. |

**Returns:**

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | Requested path (echoed back) |
| `content` | string | File content (UTF-8; non-UTF-8 bytes replaced with U+FFFD) |
| `bytes_read` | integer | Number of bytes returned in `content` |
| `truncated` | boolean | True when the file was larger than `max_bytes` |

**Security:** paths containing `..` components are rejected before any filesystem
access. Only paths under `/data/` are permitted; `/etc/`, `/proc/`, and other
system paths cannot be read through this tool. Symlink resolution is not
performed: a symlink under `/data/` that points outside `/data/` will produce
an error when opened (the kernel resolves the link), not a path validation pass.

**Example:**
```json
{ "name": "fs.read", "arguments": { "path": "/data/config.toml" } }
```

---

## net.status

Returns interface names and whether a default route is configured.

**Arguments:** none

**Returns:**

| Field | Type | Description |
|-------|------|-------------|
| `interfaces` | string[] | Interface names from `/proc/net/dev` |
| `has_default_route` | boolean | True when `/proc/net/route` contains a `00000000` destination entry |

**Example response:**
```json
{
  "interfaces": ["lo", "eth0"],
  "has_default_route": true
}
```

---

## time.now

Returns the current UTC time.

**Arguments:** none

**Returns:**

| Field | Type | Description |
|-------|------|-------------|
| `unix_seconds` | integer | Seconds since 1970-01-01T00:00:00Z |
| `rfc3339` | string | UTC time in RFC 3339 format (e.g. `2024-01-15T10:30:00Z`) |

**Example response:**
```json
{
  "unix_seconds": 1705314600,
  "rfc3339": "2024-01-15T10:30:00Z"
}
```

---

## Allowlist

All tools must be explicitly allowlisted before the model can call them.
`tools::register_all()` registers and allowlists the four tools above during
agent boot. Tools that are registered but not allowlisted are invisible to the
model.

## Adding a Tool

1. Implement `nura_core::tool::Tool` for your type (`Send+Sync` required).
2. Add the implementation under `nura-agent/src/tools/`.
3. Call `registry.register(YourTool)` and `registry.allowlist("your.tool")` in
   `tools::register_all()`.
4. Declare `read_only() -> false` if the tool mutates state (required for
   future confirmation gating).
5. Document it in this file.
