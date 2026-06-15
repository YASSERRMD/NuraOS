# NuraOS Test Reporting

After all suite jobs complete, `tests/tools/merge-reports.sh` merges the
individual suite reports into a single run view.

## Report files

| File | Description |
| --- | --- |
| `tests/reports/<suite>/<suite>-report.json` | Per-suite JSON report (RunReport struct) |
| `tests/reports/<suite>/<suite>-junit.xml` | Per-suite JUnit XML (compatible with GitHub test summary) |
| `tests/reports/<suite>/<case>-evidence/` | Per-failure evidence bundle (serial.log, metrics.txt, config.json) |
| `tests/reports/merged-report.json` | All suites merged: totals, per-suite status, all results |
| `tests/reports/job-summary.md` | Markdown table rendered into GitHub job summary |
| `tests/reports/summary.html` | Downloadable HTML artifact with colour-coded rows |
| `tests/reports/badge.json` | Badge data: `{label, status, pass_rate, commit}` for README badge |
| `tests/reports/trend.jsonl` | Append-only newline-delimited JSON trend history |

## Merged report schema

```json
{
  "commit": "abc12345...",
  "date": "2026-01-01T12:00:00Z",
  "run_url": "https://github.com/...",
  "status": "pass|fail",
  "totals": { "total": 42, "pass": 38, "fail": 2, "skip": 2 },
  "suites": [
    { "suite": "build-and-boot", "status": "pass", "pass": 6, "fail": 0, "skip": 0 }
  ],
  "results": [ ... all Result objects from all suites ... ]
}
```

## Trend file

`trend.jsonl` accumulates one JSON line per run:
```json
{"date":"2026-01-01T02:00:00Z","commit":"abc12345","status":"pass","pass":38,"fail":0,"skip":2}
```

The trend file is **not committed to git** (`tests/reports/` is gitignored). It is
generated fresh in each CI run and uploaded as an artifact for manual review. Long-term
trend history can be reconstructed from CI artifact downloads.

## Badge

`badge.json` contains:
```json
{"label": "tests", "status": "pass", "pass_rate": "95%", "commit": "abc12345"}
```

This can be used with any badge service that accepts a JSON endpoint, or read directly
in the README.

## How to invoke

```sh
# After all suites have run:
tests/tools/merge-reports.sh \
    --report-dir tests/reports \
    --commit "$(git rev-parse HEAD)" \
    --run-url "${NURA_RUN_URL}"
```

In CI, this is called as a final step in the workflow after the suite matrix completes.
See `.github/workflows/test.yml` for the integration point.

## GitHub job summary

When `GITHUB_STEP_SUMMARY` is set (standard in GitHub Actions), `merge-reports.sh`
appends the markdown summary table to the job summary automatically. No additional
configuration is needed.
