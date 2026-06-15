# Suite: tools

Verifies the tool registry exposed via the `/tools` HTTP endpoint.
Cases that require model-driven tool calls (path-traversal denial, timeout
enforcement, OS sandbox) skip when no model is loaded; those cases are
exercised end-to-end in the `e2e` suite.

## Cases

| Case | Phase | Assertion |
| --- | --- | --- |
| `tools-endpoint` | 23/24 | GET /tools returns 200 with non-empty JSON |
| `expected-tool-names` | 23/24 | All 4 built-in tools present: `fs.read`, `net.status`, `system.info`, `time.now` |
| `tool-schemas` | 24 | /tools response contains JSON Schema `type` annotations |

## Built-in tools

| Name | Read-only | Description |
| --- | --- | --- |
| `fs.read` | yes | Read a file from the filesystem (Landlock-sandboxed) |
| `net.status` | yes | Return network interface status |
| `system.info` | yes | Return CPU, memory, and OS version |
| `time.now` | yes | Return the current UTC time |

## How to run

```sh
NURA_REPO_ROOT=/path/to/nuraos tests/run-suite tools
```
