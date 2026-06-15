package reporters

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yasserrmd/nuraos/tests/harness"
)

// RunReport is the JSON structure written to disk for each suite run.
type RunReport struct {
	Suite   string           `json:"suite"`
	RunAt   string           `json:"run_at"`
	Results []harness.Result `json:"results"`
	Totals  Totals           `json:"totals"`
}

// Totals summarises the pass/fail/skip counts for a suite.
type Totals struct {
	Pass int `json:"pass"`
	Fail int `json:"fail"`
	Skip int `json:"skip"`
}

// WriteJSON writes a JSON report for run to dir/<suite>-report.json.
func WriteJSON(dir string, run harness.SuiteRun) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating report dir: %w", err)
	}

	var totals Totals
	for _, r := range run.Results {
		switch r.Status {
		case harness.StatusPass:
			totals.Pass++
		case harness.StatusFail:
			totals.Fail++
		case harness.StatusSkip:
			totals.Skip++
		}
	}

	report := RunReport{
		Suite:   run.Suite,
		RunAt:   time.Now().UTC().Format(time.RFC3339),
		Results: run.Results,
		Totals:  totals,
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling JSON report: %w", err)
	}

	out := filepath.Join(dir, run.Suite+"-report.json")
	return os.WriteFile(out, data, 0o644)
}
