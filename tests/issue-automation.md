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

## Flake detection

`tests/tools/flake-detect.sh` identifies cases that alternate between pass and
fail across recent runs and marks the corresponding GitHub issue with the
`flaky` label.

### How flake detection works

1. The merged report (`tests/reports/merged-report.json`) is scanned for all
   cases with `status == "fail"`.
2. For each failing case its `failure_signature` is extracted.
3. GitHub is searched for an open issue whose body contains that signature.
4. If the issue already carries the `flaky` label the case is skipped.
5. Otherwise the `flaky` label is applied to the existing issue.

The `flaky` label acts as a human-review gate: flaky issues are suppressed from
the re-filing loop and from auto-close so that a human can decide whether to
quarantine, fix, or ignore the instability.

### Flaky label

| Label | Colour | Description |
| --- | --- | --- |
| `flaky` | `#e4e669` (yellow) | Case shows intermittent pass/fail behaviour |

The label is created idempotently via `gh label create --force`.

### Usage reference

```sh
# Detect and label flaky issues (requires merged-report.json to exist):
tests/tools/flake-detect.sh

# Use a custom reports directory:
tests/tools/flake-detect.sh --report-dir /tmp/reports

# Adjust the look-back window and alternation threshold:
tests/tools/flake-detect.sh --window 10 --threshold 3

# Dry-run (prints gh commands without executing them):
tests/tools/flake-detect.sh --dry-run
```

`flake-detect.sh` should be run after `merge-reports.sh` in the aggregate CI
job so that the merged report is available.

## Quarantine list

`tests/tools/quarantine.json` lists test cases that are known to be flaky or
broken while a fix is in progress. Quarantined cases **run normally** but their
failures do not file new issues and do not fail the CI gate.

### Schema

```json
{
  "_comment": "...",
  "_format": {
    "suite":    "suite name matching suiteRegistry key",
    "case":     "case name within the suite",
    "reason":   "human-readable reason for quarantine",
    "expires":  "ISO 8601 date after which this entry must be reviewed",
    "filed_by": "username of the person who added this entry",
    "issue":    "optional GitHub issue number tracking the underlying flake"
  },
  "quarantined": [
    {
      "suite":    "build-and-boot",
      "case":     "data-mounted",
      "reason":   "QEMU virtio-blk timing issue on slow runners",
      "expires":  "2026-09-01",
      "filed_by": "YASSERRMD",
      "issue":    42
    }
  ]
}
```

### Rules

- Every entry **must** have a non-empty `reason` and an `expires` date.
- On the expiry date the entry must be reviewed: either removed (fixed),
  renewed with an updated date, or escalated to a blocking issue.
- Entries with no `issue` field are allowed but discouraged — prefer linking
  the tracking issue so the history is clear.
- PRs that add quarantine entries should reference the tracking issue in the
  commit message.

### How CI reads the quarantine list

The CI workflow currently enforces the quarantine gate at the issue-filing
step: `file-issue.sh` checks the quarantine list before creating or updating
an issue. A future enhancement may suppress the suite exit-code failure as well.

## Issue lifecycle summary

```
 New failure
    │
    ▼
file-issue.sh ──── is there an open issue with matching sig? ──yes──► add recurrence comment
    │                                                                      │
    no                                                                     ▼
    │                                                                 is case quarantined?
    ▼                                                                      │
 Create issue                                                        yes──► skip (no comment)
 (test-failure + suite:NAME labels)                                        │
    │                                                                      no
    │ (on next fail)                                                        │
    ▼                                                                       ▼
 Recurrence comment ◄──────────────────────────────────────────────────────┘
    │
    │ (alternating pass/fail detected)
    ▼
flake-detect.sh ──► apply "flaky" label ──► suppress re-file & auto-close

 Case passes (close-on-green)
    │
    ▼
file-issue.sh --close-on-green ──► recovery comment (+ optional auto-close)
```
