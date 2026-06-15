# Suite T14 — updates

Verifies the NuraOS A/B update subsystem: the update status endpoint, slot
information in the response, and board endpoint availability (used by the
update system to select the correct payload).

## Cases

| Case | Endpoint | Pass condition |
|------|----------|----------------|
| `update-status-endpoint` | GET /update/status | 200 |
| `active-slot-field` | GET /update/status | Non-503, body contains slot/active/current/version; or any non-empty body |
| `board-slot-info` | GET /board | 200 with non-empty body |

## Running

```
go run ./cmd/run-suite -- updates
```
