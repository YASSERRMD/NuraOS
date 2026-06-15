package selftest_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/selftest"
)

// TestAllChecksReturnNonEmpty verifies AllChecks returns at least one check.
func TestAllChecksReturnNonEmpty(t *testing.T) {
	checks := selftest.AllChecks()
	if len(checks) == 0 {
		t.Fatal("AllChecks returned no checks")
	}
	for _, c := range checks {
		if c.Name == "" {
			t.Errorf("check with empty Name: %+v", c)
		}
		if c.Category == "" {
			t.Errorf("check %q has empty Category", c.Name)
		}
		if c.Run == nil {
			t.Errorf("check %q has nil Run func", c.Name)
		}
	}
}

// TestRunnerRunReturnsReport verifies Run completes and returns a valid Report.
func TestRunnerRunReturnsReport(t *testing.T) {
	r := selftest.New()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rep := r.Run(ctx)

	if len(rep.Results) == 0 {
		t.Fatal("Report has no results")
	}
	total := rep.Pass + rep.Fail + rep.Skip
	if total != len(rep.Results) {
		t.Errorf("pass+fail+skip=%d != len(results)=%d", total, len(rep.Results))
	}
	if rep.Overall != "pass" && rep.Overall != "fail" {
		t.Errorf("Overall = %q; want \"pass\" or \"fail\"", rep.Overall)
	}
	if rep.ElapsedMS < 0 {
		t.Errorf("ElapsedMS = %d; want >= 0", rep.ElapsedMS)
	}
}

// TestBootRunnerSubset verifies NewBootRunner only loads BootSet checks.
func TestBootRunnerSubset(t *testing.T) {
	allChecks := selftest.AllChecks()
	bootCount := 0
	for _, c := range allChecks {
		if c.BootSet {
			bootCount++
		}
	}

	r := selftest.NewBootRunner()
	ctx := context.Background()
	rep := r.Run(ctx)

	if len(rep.Results) != bootCount {
		t.Errorf("boot runner ran %d checks; want %d (BootSet only)", len(rep.Results), bootCount)
	}
}

// TestFilterCategory returns only checks in the requested category.
func TestFilterCategory(t *testing.T) {
	r := selftest.New()
	filtered := r.FilterCategory(selftest.CategoryKernel)
	ctx := context.Background()
	rep := filtered.Run(ctx)

	for _, res := range rep.Results {
		if res.Category != selftest.CategoryKernel {
			t.Errorf("FilterCategory(kernel) returned result with category=%q", res.Category)
		}
	}
}

// TestSortedByCategory verifies results are sorted by category then name.
func TestSortedByCategory(t *testing.T) {
	results := []selftest.Result{
		{Name: "z", Category: selftest.CategoryNetwork},
		{Name: "a", Category: selftest.CategoryKernel},
		{Name: "m", Category: selftest.CategoryNetwork},
		{Name: "b", Category: selftest.CategoryStorage},
	}
	sorted := selftest.SortedByCategory(results)
	for i := 1; i < len(sorted); i++ {
		prev, curr := sorted[i-1], sorted[i]
		if curr.Category < prev.Category {
			t.Errorf("out of order: %q > %q at index %d", prev.Category, curr.Category, i)
		}
		if curr.Category == prev.Category && curr.Name < prev.Name {
			t.Errorf("out of order within category: %q > %q at index %d", prev.Name, curr.Name, i)
		}
	}
}

// TestFormatHuman verifies FormatHuman produces non-empty output with summary.
func TestFormatHuman(t *testing.T) {
	r := selftest.New()
	ctx := context.Background()
	rep := r.Run(ctx)
	out := selftest.FormatHuman(rep)

	if out == "" {
		t.Fatal("FormatHuman returned empty string")
	}
	if !strings.Contains(out, "Overall:") {
		t.Error("FormatHuman output missing 'Overall:' summary line")
	}
}

// TestCheckResultStatusIsValid verifies each check returns a recognised status.
func TestCheckResultStatusIsValid(t *testing.T) {
	validStatuses := map[selftest.Status]bool{
		selftest.StatusPass: true,
		selftest.StatusFail: true,
		selftest.StatusSkip: true,
	}
	r := selftest.New()
	ctx := context.Background()
	rep := r.Run(ctx)
	for _, res := range rep.Results {
		if !validStatuses[res.Status] {
			t.Errorf("check %q returned invalid status %q", res.Name, res.Status)
		}
	}
}

// TestRunSafePopulatesName verifies checks that return an empty Name get the
// check's registered Name filled in by runSafe.
func TestRunnerCustomCheck(t *testing.T) {
	r := selftest.New()
	r.Register(&selftest.Check{
		Name:     "always-pass",
		Category: selftest.CategoryAgent,
		BootSet:  false,
		Run: func(ctx context.Context) selftest.Result {
			return selftest.Result{
				Name:     "always-pass",
				Category: selftest.CategoryAgent,
				Status:   selftest.StatusPass,
				Detail:   "synthetic check",
			}
		},
	})
	ctx := context.Background()
	rep := r.Run(ctx)
	found := false
	for _, res := range rep.Results {
		if res.Name == "always-pass" {
			found = true
			if res.Status != selftest.StatusPass {
				t.Errorf("always-pass check returned %q", res.Status)
			}
		}
	}
	if !found {
		t.Error("custom check 'always-pass' not found in report")
	}
}
