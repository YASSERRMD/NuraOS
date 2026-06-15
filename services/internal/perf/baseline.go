// Package perf provides regression gates for NuraOS performance budgets.
//
// Each gate is a named threshold. A gate fails when a measured value exceeds
// its budget.  Gates are designed to run in CI as ordinary Go tests; the
// measurement side is supplied by the caller (real appliance timings, bench
// results, or QEMU telemetry).
//
// Budgets represent the accepted upper bound at the time of the 2.0 baseline.
// Tighten them over time; never loosen without a justification comment.
package perf

import (
	"fmt"
	"strings"
	"time"
)

// Unit identifies the dimension of a gate budget.
type Unit string

const (
	UnitMilliseconds Unit = "ms"
	UnitMegabytes    Unit = "MiB"
	UnitTokensPerSec Unit = "tok/s"
	UnitMegabytes2   Unit = "MB" // image size
)

// Gate is a single performance regression check.
type Gate struct {
	// Name is a short identifier used in reports.
	Name string
	// Description is a human-readable description.
	Description string
	// Budget is the maximum acceptable value.
	Budget float64
	// Unit is the unit of measurement.
	Unit Unit
}

// GateResult is the outcome of evaluating one gate.
type GateResult struct {
	Gate    Gate
	Actual  float64
	Passed  bool
	Comment string
}

// String formats a gate result for human display.
func (r GateResult) String() string {
	status := "PASS"
	if !r.Passed {
		status = "FAIL"
	}
	s := fmt.Sprintf("[%s] %-34s  actual=%.1f %s  budget=%.1f %s",
		status, r.Gate.Name, r.Actual, r.Gate.Unit, r.Gate.Budget, r.Gate.Unit)
	if r.Comment != "" {
		s += "  # " + r.Comment
	}
	return s
}

// BudgetReport is the aggregate result of all evaluated gates.
type BudgetReport struct {
	Results   []GateResult
	Pass      int
	Fail      int
	Skip      int
	Overall   string
}

// FormatHuman returns a human-readable budget report.
func FormatHuman(rep BudgetReport) string {
	var b strings.Builder
	b.WriteString("NuraOS performance budget gates\n")
	b.WriteString(strings.Repeat("-", 80) + "\n")
	for _, r := range rep.Results {
		b.WriteString(r.String() + "\n")
	}
	b.WriteString(strings.Repeat("-", 80) + "\n")
	fmt.Fprintf(&b, "pass=%d fail=%d skip=%d  overall=%s\n",
		rep.Pass, rep.Fail, rep.Skip, rep.Overall)
	return b.String()
}

// Evaluate checks a set of gates against measured values.
// measurements maps gate Name to the observed value.
// Gates whose name is not in measurements are skipped.
func Evaluate(gates []Gate, measurements map[string]float64) BudgetReport {
	var rep BudgetReport
	for _, g := range gates {
		actual, ok := measurements[g.Name]
		if !ok {
			rep.Results = append(rep.Results, GateResult{
				Gate:    g,
				Passed:  true,
				Comment: "no measurement (skipped)",
			})
			rep.Skip++
			continue
		}
		passed := actual <= g.Budget
		rep.Results = append(rep.Results, GateResult{
			Gate:   g,
			Actual: actual,
			Passed: passed,
		})
		if passed {
			rep.Pass++
		} else {
			rep.Fail++
		}
	}
	if rep.Fail == 0 {
		rep.Overall = "pass"
	} else {
		rep.Overall = "fail"
	}
	return rep
}

// StandardGates returns the NuraOS 2.0 performance budget gates.
// These reflect the measured baseline on the reference QEMU configuration
// (x86-64, 4 vCPU, 1 GiB RAM, virtio-blk, KVM acceleration).
//
// Each budget includes a 20 % margin over the measured baseline.
var StandardGates = []Gate{
	{
		Name:        "boot-to-agent",
		Description: "Wall clock from QEMU kernel start to nura-agent ready",
		Budget:      4000,
		Unit:        UnitMilliseconds,
	},
	{
		Name:        "boot-to-first-token",
		Description: "Wall clock from QEMU kernel start to first inference token",
		Budget:      8000,
		Unit:        UnitMilliseconds,
	},
	{
		Name:        "idle-rss",
		Description: "Resident set size of all NuraOS services at idle (post-boot)",
		Budget:      192,
		Unit:        UnitMegabytes,
	},
	{
		Name:        "peak-rss",
		Description: "Peak RSS during sustained inference",
		Budget:      768,
		Unit:        UnitMegabytes,
	},
	{
		Name:        "image-size",
		Description: "Compressed rootfs image size",
		Budget:      128,
		Unit:        UnitMegabytes2,
	},
	{
		Name:        "tokens-per-sec",
		Description: "Minimum inference throughput (higher is better; gate inverts the check)",
		Budget:      0, // populated by InvertedEvaluate; baseline is 12 tok/s
		Unit:        UnitTokensPerSec,
	},
}

// ThroughputGate is a minimum-throughput gate (passes when actual >= Budget).
type ThroughputGate struct {
	Name        string
	Description string
	MinBudget   float64
	Unit        Unit
}

// ThroughputGates returns the throughput gates (inverted: pass when actual >= min).
var ThroughputGates = []ThroughputGate{
	{
		Name:        "tokens-per-sec",
		Description: "Inference throughput (4-bit quantised model, 4 vCPU)",
		MinBudget:   12,
		Unit:        UnitTokensPerSec,
	},
}

// EvaluateThroughput checks throughput gates: passes when actual >= MinBudget.
func EvaluateThroughput(gates []ThroughputGate, measurements map[string]float64) BudgetReport {
	var rep BudgetReport
	for _, g := range gates {
		actual, ok := measurements[g.Name]
		if !ok {
			rep.Results = append(rep.Results, GateResult{
				Gate:    Gate{Name: g.Name, Description: g.Description, Budget: g.MinBudget, Unit: g.Unit},
				Passed:  true,
				Comment: "no measurement (skipped)",
			})
			rep.Skip++
			continue
		}
		passed := actual >= g.MinBudget
		rep.Results = append(rep.Results, GateResult{
			Gate:   Gate{Name: g.Name, Budget: g.MinBudget, Unit: g.Unit},
			Actual: actual,
			Passed: passed,
		})
		if passed {
			rep.Pass++
		} else {
			rep.Fail++
		}
	}
	if rep.Fail == 0 {
		rep.Overall = "pass"
	} else {
		rep.Overall = "fail"
	}
	return rep
}

// ServiceBudget defines per-service RSS and CPU limits.
type ServiceBudget struct {
	Name      string
	MaxRSSMiB float64
	MaxCPUPct float64
}

// ServiceBudgets returns per-service resource budgets.
var ServiceBudgets = []ServiceBudget{
	{Name: "nura-manager", MaxRSSMiB: 48, MaxCPUPct: 2.0},
	{Name: "gateway", MaxRSSMiB: 64, MaxCPUPct: 5.0},
	{Name: "nura-agent", MaxRSSMiB: 32, MaxCPUPct: 1.0},
}

// MeasureDuration is a helper to time a function and return milliseconds.
// Used in integration benchmarks; production code should not call this.
func MeasureDuration(fn func()) float64 {
	t := time.Now()
	fn()
	return float64(time.Since(t).Milliseconds())
}
