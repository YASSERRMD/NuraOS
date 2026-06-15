package perf_test

import (
	"os"
	"strconv"
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/perf"
)

// TestStandardGatesDefinedAndNonZero verifies that all standard gates have a
// non-empty name and a positive budget.
func TestStandardGatesDefinedAndNonZero(t *testing.T) {
	for _, g := range perf.StandardGates {
		if g.Name == "" {
			t.Error("gate with empty name found")
		}
		// tokens-per-sec gate budget is 0 in StandardGates (it uses ThroughputGates).
		if g.Name == "tokens-per-sec" {
			continue
		}
		if g.Budget <= 0 {
			t.Errorf("gate %q has non-positive budget %v", g.Name, g.Budget)
		}
	}
}

// TestEvaluateAllSkipWhenNoMeasurements verifies that missing measurements are
// skipped, not failed, so CI never fails without telemetry.
func TestEvaluateAllSkipWhenNoMeasurements(t *testing.T) {
	rep := perf.Evaluate(perf.StandardGates, nil)
	if rep.Fail > 0 {
		t.Errorf("Evaluate with no measurements: Fail=%d; want 0 (all should skip)", rep.Fail)
	}
	if rep.Overall != "pass" {
		t.Errorf("Overall = %q; want \"pass\" when no measurements", rep.Overall)
	}
}

// TestEvaluatePassWhenWithinBudget verifies the gate passes when the measured
// value is at or below the budget.
func TestEvaluatePassWhenWithinBudget(t *testing.T) {
	m := map[string]float64{
		"boot-to-agent": 3500, // budget is 4000
		"idle-rss":      120,  // budget is 192
	}
	rep := perf.Evaluate(perf.StandardGates, m)
	for _, r := range rep.Results {
		if r.Gate.Name == "boot-to-agent" || r.Gate.Name == "idle-rss" {
			if !r.Passed {
				t.Errorf("gate %q: expected pass (actual=%.1f, budget=%.1f)", r.Gate.Name, r.Actual, r.Gate.Budget)
			}
		}
	}
}

// TestEvaluateFailWhenOverBudget verifies the gate fails when the measured
// value exceeds the budget.
func TestEvaluateFailWhenOverBudget(t *testing.T) {
	m := map[string]float64{
		"boot-to-agent": 5000, // budget is 4000; should fail
	}
	rep := perf.Evaluate(perf.StandardGates, m)
	found := false
	for _, r := range rep.Results {
		if r.Gate.Name == "boot-to-agent" {
			found = true
			if r.Passed {
				t.Error("boot-to-agent: expected fail when actual 5000 > budget 4000")
			}
		}
	}
	if !found {
		t.Error("boot-to-agent gate not found in results")
	}
	if rep.Overall != "fail" {
		t.Errorf("Overall = %q; want \"fail\"", rep.Overall)
	}
}

// TestThroughputGatePassWhenAboveMin verifies throughput gate passes when
// actual throughput exceeds the minimum.
func TestThroughputGatePassWhenAboveMin(t *testing.T) {
	m := map[string]float64{"tokens-per-sec": 15} // min is 12
	rep := perf.EvaluateThroughput(perf.ThroughputGates, m)
	if rep.Fail > 0 {
		t.Errorf("throughput gate failed at 15 tok/s (min=12)")
	}
}

// TestThroughputGateFailWhenBelowMin verifies throughput gate fails when
// actual throughput is below the minimum.
func TestThroughputGateFailWhenBelowMin(t *testing.T) {
	m := map[string]float64{"tokens-per-sec": 8} // min is 12
	rep := perf.EvaluateThroughput(perf.ThroughputGates, m)
	if rep.Fail == 0 {
		t.Error("throughput gate should fail at 8 tok/s (min=12)")
	}
}

// TestFormatHumanContainsGateNames verifies the formatted report includes all
// gate names.
func TestFormatHumanContainsGateNames(t *testing.T) {
	rep := perf.Evaluate(perf.StandardGates, nil)
	out := perf.FormatHuman(rep)
	for _, g := range perf.StandardGates {
		if g.Name == "tokens-per-sec" {
			continue
		}
		found := false
		for _, line := range splitLines(out) {
			if contains(line, g.Name) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("gate %q not found in FormatHuman output", g.Name)
		}
	}
}

// TestCIBootGate is the actual CI regression gate for boot-to-agent latency.
// It reads NURA_BOOT_MS from the environment.  If not set the gate is skipped.
// Set NURA_BOOT_MS=<milliseconds> in CI after measuring appliance boot time.
func TestCIBootGate(t *testing.T) {
	raw := os.Getenv("NURA_BOOT_MS")
	if raw == "" {
		t.Skip("NURA_BOOT_MS not set; skip boot latency gate")
	}
	ms, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		t.Fatalf("NURA_BOOT_MS=%q: not a number: %v", raw, err)
	}
	rep := perf.Evaluate(perf.StandardGates, map[string]float64{"boot-to-agent": ms})
	for _, r := range rep.Results {
		if r.Gate.Name == "boot-to-agent" && !r.Passed {
			t.Errorf("boot-to-agent regression: %s", r.String())
		}
	}
}

// TestCIFootprintGate is the CI regression gate for idle RSS.
// Set NURA_IDLE_RSS_MIB=<MiB> in CI.
func TestCIFootprintGate(t *testing.T) {
	raw := os.Getenv("NURA_IDLE_RSS_MIB")
	if raw == "" {
		t.Skip("NURA_IDLE_RSS_MIB not set; skip footprint gate")
	}
	mib, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		t.Fatalf("NURA_IDLE_RSS_MIB=%q: not a number: %v", raw, err)
	}
	rep := perf.Evaluate(perf.StandardGates, map[string]float64{"idle-rss": mib})
	for _, r := range rep.Results {
		if r.Gate.Name == "idle-rss" && !r.Passed {
			t.Errorf("idle-rss regression: %s", r.String())
		}
	}
}

// TestCIThroughputGate is the CI regression gate for inference throughput.
// Set NURA_TOKENS_PER_SEC=<tok/s> in CI.
func TestCIThroughputGate(t *testing.T) {
	raw := os.Getenv("NURA_TOKENS_PER_SEC")
	if raw == "" {
		t.Skip("NURA_TOKENS_PER_SEC not set; skip throughput gate")
	}
	tps, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		t.Fatalf("NURA_TOKENS_PER_SEC=%q: not a number: %v", raw, err)
	}
	rep := perf.EvaluateThroughput(perf.ThroughputGates, map[string]float64{"tokens-per-sec": tps})
	if rep.Fail > 0 {
		t.Errorf("tokens-per-sec regression: actual=%.1f tok/s, min=12 tok/s", tps)
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
