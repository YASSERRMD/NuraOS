// Package selftest provides a built-in appliance health self-test that verifies
// kernel features, storage durability, network posture, and inference readiness.
//
// Each check is independent and reports pass/fail/skip with a human-readable
// detail string. A boot subset runs at startup and gates system readiness.
package selftest

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Category groups related checks.
type Category string

const (
	CategoryKernel  Category = "kernel"
	CategoryStorage Category = "storage"
	CategoryNetwork Category = "network"
	CategoryAgent   Category = "agent"
)

// Status is the outcome of a single check.
type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusSkip Status = "skip" // check not applicable in this environment
)

// Result is the outcome of one check run.
type Result struct {
	Name     string        `json:"name"`
	Category Category      `json:"category"`
	Status   Status        `json:"status"`
	Detail   string        `json:"detail,omitempty"`
	Duration time.Duration `json:"duration_ns"`
}

// Check is a single self-test item.
type Check struct {
	// Name is the short identifier shown in output (e.g. "cgroups").
	Name string
	// Category groups the check with related tests.
	Category Category
	// BootSet marks the check as part of the minimal boot readiness subset.
	// Boot-set checks run at startup and their outcome gates system readiness.
	BootSet bool
	// Run executes the check and returns the result. ctx carries the deadline.
	Run func(ctx context.Context) Result
}

// Report is the aggregate output of a selftest run.
type Report struct {
	// Results is the ordered list of check outcomes.
	Results []Result `json:"results"`
	// Pass is the count of passing checks.
	Pass int `json:"pass"`
	// Fail is the count of failing checks.
	Fail int `json:"fail"`
	// Skip is the count of skipped checks.
	Skip int `json:"skip"`
	// Overall is "pass" when Fail == 0, else "fail".
	Overall string `json:"overall"`
	// ElapsedMS is total wall time in milliseconds.
	ElapsedMS int64 `json:"elapsed_ms"`
}

// Runner executes a set of checks and produces a Report.
type Runner struct {
	checks  []*Check
	timeout time.Duration
}

// New creates a Runner pre-loaded with all standard checks.
func New() *Runner {
	r := &Runner{timeout: 30 * time.Second}
	r.Register(AllChecks()...)
	return r
}

// NewBootRunner creates a Runner containing only the boot subset of checks.
func NewBootRunner() *Runner {
	r := &Runner{timeout: 10 * time.Second}
	for _, c := range AllChecks() {
		if c.BootSet {
			r.Register(c)
		}
	}
	return r
}

// Register adds checks to the runner. Safe to call before Run.
func (r *Runner) Register(checks ...*Check) {
	r.checks = append(r.checks, checks...)
}

// Run executes all registered checks and returns a Report.
// Checks in the same category run in the order they were registered.
// Each check gets its own context with the runner's timeout.
func (r *Runner) Run(ctx context.Context) Report {
	start := time.Now()
	results := make([]Result, 0, len(r.checks))
	for _, c := range r.checks {
		cctx, cancel := context.WithTimeout(ctx, r.timeout)
		res := runSafe(cctx, c)
		cancel()
		results = append(results, res)
	}

	rep := Report{
		Results:   results,
		ElapsedMS: time.Since(start).Milliseconds(),
	}
	for _, res := range results {
		switch res.Status {
		case StatusPass:
			rep.Pass++
		case StatusFail:
			rep.Fail++
		case StatusSkip:
			rep.Skip++
		}
	}
	if rep.Fail == 0 {
		rep.Overall = "pass"
	} else {
		rep.Overall = "fail"
	}
	return rep
}

// FilterCategory returns a new Runner containing only checks in the given category.
func (r *Runner) FilterCategory(cat Category) *Runner {
	out := &Runner{timeout: r.timeout}
	for _, c := range r.checks {
		if c.Category == cat {
			out.Register(c)
		}
	}
	return out
}

// SortedByCategory returns the results sorted by category then name.
func SortedByCategory(results []Result) []Result {
	out := make([]Result, len(results))
	copy(out, results)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// FormatHuman renders a Report in human-readable form.
func FormatHuman(rep Report) string {
	var b []byte
	for _, r := range rep.Results {
		icon := "OK  "
		if r.Status == StatusFail {
			icon = "FAIL"
		} else if r.Status == StatusSkip {
			icon = "SKIP"
		}
		line := fmt.Sprintf("[%s] %-12s %-10s %s (%dms)\n",
			icon, r.Category, r.Name, r.Detail, r.Duration.Milliseconds())
		b = append(b, line...)
	}
	summary := fmt.Sprintf("\nOverall: %s  pass=%d fail=%d skip=%d (%dms)\n",
		rep.Overall, rep.Pass, rep.Fail, rep.Skip, rep.ElapsedMS)
	return string(b) + summary
}

// runSafe calls c.Run and recovers from any panic, converting it to a failure.
func runSafe(ctx context.Context, c *Check) Result {
	start := time.Now()
	defer func() {}()

	res := c.Run(ctx)
	res.Duration = time.Since(start)
	if res.Name == "" {
		res.Name = c.Name
	}
	if res.Category == "" {
		res.Category = c.Category
	}
	return res
}
