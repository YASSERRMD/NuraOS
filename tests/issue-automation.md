# NuraOS Test Issue Automation

GitHub issues for failing test cases are created and maintained automatically
by `tests/tools/file-issue.sh`. This document describes the labels used, the
deduplication logic, the close-on-green behaviour, and the token scopes required.

## How it works

Each failing test case produces a `Result` JSON with a stable `failure_signature`
field (see `tests/README.md` for the schema). The CI workflow passes the JSON to
`file-issue.sh` after a suite finishes.

### New failure

1. `file-issue.sh` searches GitHub for open issues whose body contains
   `nuraos-sig: SIGNATURE` (an HTML comment invisible to readers).
2. If no match is found: a new issue is created titled `[suite] case failing`
   with the error summary, commit SHA, run link, and the hidden marker.
3. If a match is found: a recurrence comment is added to the existing issue.
   No duplicate issue is opened.

### Recovery

When a previously failing case passes, the workflow calls
`file-issue.sh --close-on-green`. The script searches for an open issue titled
`[suite] case failing`, adds a recovery comment, and optionally auto-closes
the issue with `--auto-close`.

## Labels

| Label | Colour | Description |
| --- | --- | --- |
| `test-failure` | `#d73a4a` (red) | Applied to every auto-filed failure issue |
| `suite:NAME` | `#0075ca` (blue) | One per subsystem suite; created on first use |

Labels are created idempotently (`gh label create --force`) so no manual setup
is required.

## Deduplication

The dedup key is the `failure_signature`: a 16-char hex SHA-256 of
`suite:case:Normalise(message)` where `Normalise` strips timestamps, PIDs,
hex addresses, and high-numbered ports. The same logical failure produces the
same signature across runs, so a single issue accumulates all occurrences.

The marker is embedded as an HTML comment in the issue body:
```
<!-- nuraos-sig: SIGNATURE -->
```

Search query used:
```
"nuraos-sig: SIGNATURE" in:body
```

## Dry-run mode

Pass `--dry-run` to print all `gh` commands without executing them. `jq` is
still required (to parse the result JSON). This is the default for local
developer runs; the CI workflow omits `--dry-run` on main/nightly and adds
it back for PR runs (where the goal is annotation only).

## Required token scopes

The `GH_TOKEN` or `GITHUB_TOKEN` used by `gh` must have:

| Scope | Used for |
| --- | --- |
| `issues:write` | Create issues, add comments, close issues, create labels |

In GitHub Actions the built-in `GITHUB_TOKEN` is sufficient when the workflow
grants `issues: write` permission.

## Usage reference

```sh
# File or update an issue for a failing result:
tests/tools/file-issue.sh result.json

# Dry-run (no API calls):
tests/tools/file-issue.sh --dry-run result.json

# Mark a recovered case (comment on the existing issue):
tests/tools/file-issue.sh --close-on-green result.json

# Mark a recovered case and auto-close the issue:
tests/tools/file-issue.sh --close-on-green --auto-close result.json

# Set the CI run URL in the body/comment:
NURA_RUN_URL="https://github.com/OWNER/REPO/actions/runs/123" \
  tests/tools/file-issue.sh result.json
```
