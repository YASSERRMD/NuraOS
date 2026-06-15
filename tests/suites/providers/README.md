# Suite: providers

Verifies the provider abstraction and the `/chat` HTTP endpoint contract.
Cases that require a loaded language model or remote API keys skip automatically
when those prerequisites are absent.

## Cases

| Case | Phase | Assertion | Skip condition |
| --- | --- | --- | --- |
| `bad-content-type` | 28 | Non-JSON Content-Type → 415 | Never |
| `empty-messages` | 28 | Empty message list → 400 | Never |
| `invalid-json` | 28 | Malformed JSON body → 400 | Never |
| `local-conformance` | 15-17 | Local provider SSE stream has `done` event | No model in `/data/models` |
| `streaming-deltas` | 17 | `token_delta` events appear before `done` | No model |
| `provider-override` | 18/19 | `provider=local` in request routes correctly | No model |
| `anthropic-conformance` | 20 | Anthropic SSE stream conforms | `ANTHROPIC_API_KEY` absent |
| `openai-conformance` | 21 | OpenAI-compat SSE stream conforms | `OPENAI_API_KEY` absent |
| `routing-models` | 18/39 | `/models` returns JSON with `available` field | Never |

## SSE event format

The `/chat` endpoint streams Server-Sent Events where each line is:
```
data: {"kind":"token_delta","text":"..."}
data: {"kind":"done","stop_reason":"end_of_turn"}
```

Canonical `kind` values: `token_delta`, `tool_call_delta`, `usage`, `done`, `error`.

## How to run

```sh
# Local-only (no model, skips completion cases):
NURA_REPO_ROOT=/path/to/nuraos tests/run-suite providers

# With remote providers:
ANTHROPIC_API_KEY=sk-ant-... OPENAI_API_KEY=sk-... \
  NURA_REPO_ROOT=/path/to/nuraos tests/run-suite providers
```
