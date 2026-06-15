# NuraOS Gateway API

The Go HTTP gateway exposes the agent's capabilities over a plain HTTP/1.1 API.
It listens on port 8080 by default; set `GATEWAY_PORT` to override.

When running under QEMU with the recommended port forward, use
`http://localhost:18080` on the host.

## Endpoints

### GET /healthz

Probes the agent socket and returns overall service health.

**Response 200** (agent reachable):
```json
{
  "status": "ok",
  "agent_reachable": true,
  "agent": {
    "status": "ok",
    "provider": "anthropic",
    "uptime_seconds": 312
  }
}
```

**Response 503** (agent not running):
```json
{
  "status": "degraded",
  "agent_reachable": false
}
```

```sh
curl -s http://localhost:18080/healthz | jq .
```

---

### GET /version

Returns the gateway's build-time version.

**Response 200**:
```json
{
  "service": "nura-gateway",
  "version": "0.1.0"
}
```

```sh
curl -s http://localhost:18080/version
```

---

### POST /chat

Streams a conversation turn as Server-Sent Events (SSE).

**Request headers**:
- `Content-Type: application/json` (required)

**Request body** (max 64 KiB):
```json
{
  "messages": [
    { "role": "user", "content": "What is the current CPU load?" }
  ],
  "max_tokens": 512,
  "temperature": 0.7,
  "provider": ""
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| messages | array | yes | Conversation history; must not be empty |
| max_tokens | int | no | Token ceiling for the response |
| temperature | float | no | Sampling temperature (0.0-1.0) |
| provider | string | no | Provider hint; overrides routing when supported |

**Response 200** (streaming, `Content-Type: text/event-stream`):

Each SSE frame carries a JSON-encoded `TurnEvent`:

```
data: {"type":"token","text":"The current 1-minute load average is "}

data: {"type":"token","text":"0.42."}

data: {"type":"done"}

```

Event types:

| type | Fields | Meaning |
|------|--------|---------|
| token | text | Incremental text from the model |
| usage | (reserved) | Token usage summary (future) |
| done | | Stream complete |
| error | message | Agent-side error |

**Client disconnect**: cancelling the HTTP request (closing the connection)
propagates to the agent via context cancellation. The in-flight turn is aborted.

**Error responses**:
- `400 Bad Request` - invalid JSON or empty messages
- `413 Request Entity Too Large` - body exceeds 64 KiB
- `415 Unsupported Media Type` - missing or wrong Content-Type
- `503 Service Unavailable` - agent not reachable

```sh
# Stream a completion
curl -N -X POST http://localhost:18080/chat \
  -H "Content-Type: application/json" \
  -d '{"messages":[{"role":"user","content":"What time is it?"}]}' \
  --no-buffer

# Multi-turn conversation
curl -N -X POST http://localhost:18080/chat \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "user", "content": "What is my hostname?"},
      {"role": "assistant", "content": "Your hostname is nura-os."},
      {"role": "user", "content": "And my uptime?"}
    ]
  }'

# With provider override
curl -N -X POST http://localhost:18080/chat \
  -H "Content-Type: application/json" \
  -d '{"messages":[{"role":"user","content":"hello"}],"provider":"anthropic"}'
```

---

### GET /tools

Returns the list of tools currently registered and allowlisted in the agent.

**Response 200**:
```json
{
  "tools": [
    {
      "name": "system.info",
      "description": "Returns hostname, uptime, memory, and load averages.",
      "read_only": true,
      "schema": {}
    },
    {
      "name": "fs.read",
      "description": "Reads a file from the allowlisted paths.",
      "read_only": true,
      "schema": {}
    },
    {
      "name": "net.status",
      "description": "Lists network interfaces and default route status.",
      "read_only": true,
      "schema": {}
    },
    {
      "name": "time.now",
      "description": "Returns the current UTC time in RFC 3339 format.",
      "read_only": true,
      "schema": {}
    }
  ]
}
```

```sh
curl -s http://localhost:18080/tools | jq '.tools[].name'
```

---

## Request size limits

| Limit | Value |
|-------|-------|
| POST /chat body | 64 KiB |
| Server read timeout | 10 s |
| Server write timeout | 10 s (cleared for SSE) |
| Server idle timeout | 60 s |

## SSE client recipe (shell)

```sh
# Parse SSE token events and print text as it arrives.
curl -sN -X POST http://localhost:18080/chat \
  -H "Content-Type: application/json" \
  -d '{"messages":[{"role":"user","content":"Summarise my system status."}]}' |
  while IFS= read -r line; do
    case "$line" in
      data:*)
        payload="${line#data: }"
        type=$(printf '%s' "$payload" | grep -o '"type":"[^"]*"' | cut -d'"' -f4)
        text=$(printf '%s' "$payload" | grep -o '"text":"[^"]*"' | cut -d'"' -f4)
        [ "$type" = "token" ] && printf '%s' "$text"
        [ "$type" = "done" ]  && printf '\n'
        ;;
    esac
  done
```
