# NuraOS Provenance Log

## Purpose

Every interaction with the NuraOS agent is recorded in an append-only JSONL
log stored under `/data/sessions/`. Each entry carries the hash of the previous
entry, forming a tamper-evident chain: deleting, reordering, or editing any
past line is detectable without a network connection or trusted third party.

## File layout

```
/data/sessions/
  session-0001.jsonl   <- first file; starts at seq=1
  session-0002.jsonl   <- rotated when 0001 exceeds 10 MiB; chain continues
  ...
```

Files are rotated at 10 MiB. The hash chain spans files: the last entry in
`session-N.jsonl` has a `prev_hash` equal to the last entry's `hash` in
`session-(N-1).jsonl`.

## Entry format

Each line is a JSON object followed by a newline (`\n`). Fields:

| Field | Type | Description |
|-------|------|-------------|
| `seq` | integer | Monotonically increasing sequence number across all files (starts at 1) |
| `ts` | string | UTC timestamp, ISO-8601 (`YYYY-MM-DDTHH:MM:SSZ`) |
| `kind` | string | Event category (see below) |
| `turn_id` | string | UUID of the agent turn this event belongs to |
| `content` | object | Event payload; schema depends on `kind` |
| `prev_hash` | string | `hash` from the previous entry, or 64 hex zeros for the first entry |
| `hash` | string | SHA-256(prev_hash_bytes || canonical_payload_bytes), hex-encoded |

### Event kinds

| `kind` | When recorded | `content` fields |
|--------|---------------|-----------------|
| `turn_start` | Prompt received | `prompt` (string) |
| `tool_call` | Tool invoked | `tool` (name), `args` (object) |
| `tool_result` | Tool returned | `tool` (name), `result` (string or object) |
| `completion` | Model response | `text` (string), `tokens_in` (int), `tokens_out` (int) |
| `turn_end` | Turn finished | `elapsed_ms` (int) |

### Example entries

```json
{"seq":1,"ts":"2024-01-15T10:23:00Z","kind":"turn_start","turn_id":"a1b2c3d4-...","content":{"prompt":"Hello"},"prev_hash":"0000000000000000000000000000000000000000000000000000000000000000","hash":"3a7b..."}
{"seq":2,"ts":"2024-01-15T10:23:01Z","kind":"completion","turn_id":"a1b2c3d4-...","content":{"text":"Hi!","tokens_in":5,"tokens_out":3},"prev_hash":"3a7b...","hash":"9f4c..."}
{"seq":3,"ts":"2024-01-15T10:23:01Z","kind":"turn_end","turn_id":"a1b2c3d4-...","content":{"elapsed_ms":950},"prev_hash":"9f4c...","hash":"e2d1..."}
```

## Hash computation

The hash for entry `N` is:

```
hash_N = SHA-256(prev_hash_bytes || canonical_payload_bytes)
```

where:

- `prev_hash_bytes` = the ASCII bytes of the previous entry's `hash` hex string
  (or 64 ASCII `0` bytes for the first entry).
- `canonical_payload_bytes` = `serde_json::to_vec` of a struct containing
  **all fields except `hash`**: `{seq, ts, kind, turn_id, content, prev_hash}`.
  Field order is deterministic (struct declaration order in Rust).

The hash is stored as a 64-character lowercase hex string (256 bits).

## Verifying the chain

```sh
nura-agent verify-provenance
# or with a custom directory:
nura-agent verify-provenance --dir /data/sessions
```

Exit code 0 means all entries verified. Exit code 1 means at least one entry
failed verification; the error message names the file, line number, seq, and
failure mode.

### What is detected

| Tampering | Detection mechanism |
|-----------|---------------------|
| Editing any field in a past entry | `hash` recompute mismatch |
| Deleting an entry | `seq` mismatch on the next entry |
| Inserting a synthetic entry | `prev_hash` or `seq` mismatch |
| Truncating the file mid-chain | Restarting `seq` from 1 in a new session overwrites the gap |
| Reordering entries within a file | `prev_hash` mismatch |

### What is NOT detected

- Appending entries after the last real entry (undetectable without an external
  anchor, such as a signed checkpoint).
- Wholesale replacement of the entire directory (mitigated by keeping an
  off-device backup of the genesis hash or a periodic checkpoint).

## What is never recorded

The provenance log records the **structure** of interactions, not raw secrets:

- API keys and gateway tokens are never written to provenance.
- At INFO level (the default), completion text is redacted; use DEBUG only
  during development.
- Tool arguments are included at the DEBUG schema level; operators should
  consider this when deciding log retention.

## Implementation

The provenance writer lives in `nura-core::provenance`. Key types:

- `ProvenanceWriter::open(dir)` -- opens or creates the log directory and
  positions the writer at the end of the current file.
- `ProvenanceWriter::append(kind, turn_id, content)` -- appends one entry;
  flushes to disk; rotates if needed.
- `verify_chain(dir)` -- walks all files and verifies the chain; returns total
  entry count or an error description.
