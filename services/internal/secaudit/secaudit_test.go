package secaudit_test

import (
	"context"
	"strings"
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/secaudit"
)

// TestNewAuditorHasChecks verifies the default auditor has at least one check.
func TestNewAuditorHasChecks(t *testing.T) {
	a := secaudit.New()
	ctx := context.Background()
	rep := a.Run(ctx)
	if len(rep.Findings) == 0 {
		t.Fatal("AuditReport has no findings")
	}
}

// TestAuditReportTotalsMatchFindings verifies pass+fail+warn+skip = len(Findings).
func TestAuditReportTotalsMatchFindings(t *testing.T) {
	a := secaudit.New()
	ctx := context.Background()
	rep := a.Run(ctx)

	total := rep.Pass + rep.Fail + rep.Warn + rep.Skip
	if total != len(rep.Findings) {
		t.Errorf("pass+fail+warn+skip=%d != len(findings)=%d", total, len(rep.Findings))
	}
}

// TestAuditReportOverallPassWhenNoCritical verifies overall="pass" when
// Critical == 0.
func TestAuditReportOverallPassWhenNoCritical(t *testing.T) {
	a := secaudit.New()
	ctx := context.Background()
	rep := a.Run(ctx)
	if rep.Critical == 0 && rep.Overall != "pass" {
		t.Errorf("Overall = %q with Critical=0; want \"pass\"", rep.Overall)
	}
	if rep.Critical > 0 && rep.Overall != "fail" {
		t.Errorf("Overall = %q with Critical=%d; want \"fail\"", rep.Overall, rep.Critical)
	}
}

// TestFindingStatusesAreValid verifies every finding has a recognised status.
func TestFindingStatusesAreValid(t *testing.T) {
	valid := map[secaudit.Status]bool{
		secaudit.StatusPass: true,
		secaudit.StatusFail: true,
		secaudit.StatusWarn: true,
		secaudit.StatusSkip: true,
	}
	a := secaudit.New()
	ctx := context.Background()
	rep := a.Run(ctx)
	for _, f := range rep.Findings {
		if !valid[f.Status] {
			t.Errorf("finding %q has invalid status %q", f.Name, f.Status)
		}
	}
}

// TestFormatHumanContainsSummary verifies FormatHuman produces non-empty output.
func TestFormatHumanContainsSummary(t *testing.T) {
	a := secaudit.New()
	ctx := context.Background()
	rep := a.Run(ctx)
	out := secaudit.FormatHuman(rep)
	if out == "" {
		t.Fatal("FormatHuman returned empty string")
	}
	if !strings.Contains(out, "Overall:") {
		t.Error("FormatHuman output missing 'Overall:' summary line")
	}
}

// TestFilterBySeverityCriticalOnly verifies only critical checks are run.
func TestFilterBySeverityCriticalOnly(t *testing.T) {
	a := secaudit.New()
	filtered := a.FilterBySeverity(secaudit.SeverityCritical)
	ctx := context.Background()
	rep := filtered.Run(ctx)
	for _, f := range rep.Findings {
		if f.Severity != secaudit.SeverityCritical {
			t.Errorf("non-critical finding %q with severity %q in critical-only run",
				f.Name, f.Severity)
		}
	}
}

// TestCheckPathsExistingPath verifies an existing path passes.
func TestCheckPathsExistingPath(t *testing.T) {
	findings := secaudit.CheckPaths([]string{"/tmp"})
	if len(findings) != 1 {
		t.Fatalf("CheckPaths returned %d findings; want 1", len(findings))
	}
	if findings[0].Status != secaudit.StatusPass {
		t.Errorf("CheckPaths(/tmp) = %q; want pass", findings[0].Status)
	}
}

// TestCheckPathsMissingPath verifies a missing path fails.
func TestCheckPathsMissingPath(t *testing.T) {
	findings := secaudit.CheckPaths([]string{"/nonexistent-secaudit-path-xyz"})
	if len(findings) != 1 {
		t.Fatalf("CheckPaths returned %d findings; want 1", len(findings))
	}
	if findings[0].Status != secaudit.StatusFail {
		t.Errorf("CheckPaths(missing) = %q; want fail", findings[0].Status)
	}
}

// TestCustomCheck verifies a user-registered check appears in results.
func TestCustomCheck(t *testing.T) {
	a := secaudit.New()
	a.Register(&secaudit.Check{
		Name:     "always-pass",
		Category: "test",
		Severity: secaudit.SeverityInfo,
		Run: func(ctx context.Context) secaudit.Finding {
			return secaudit.Finding{
				Name:     "always-pass",
				Category: "test",
				Severity: secaudit.SeverityInfo,
				Status:   secaudit.StatusPass,
				Detail:   "synthetic test check",
			}
		},
	})
	ctx := context.Background()
	rep := a.Run(ctx)
	found := false
	for _, f := range rep.Findings {
		if f.Name == "always-pass" {
			found = true
			if f.Status != secaudit.StatusPass {
				t.Errorf("custom check returned %q; want pass", f.Status)
			}
		}
	}
	if !found {
		t.Error("custom check 'always-pass' not found in report")
	}
}
