// run-suite boots NuraOS in QEMU, runs a named test suite, and writes JUnit
// XML and JSON reports to tests/reports/<suite>/.
//
// Usage:
//
//	run-suite <suite-name>
//
// Example:
//
//	run-suite smoke
//	run-suite build-and-boot
//
// The repo root is discovered automatically by walking up from the working
// directory. Override with the NURA_REPO_ROOT environment variable.
//
// Reports are written to tests/reports/<suite>/ relative to the repo root.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/yasserrmd/nuraos/tests/harness"
	"github.com/yasserrmd/nuraos/tests/reporters"
	agentcore "github.com/yasserrmd/nuraos/tests/suites/agent-core"
	buildandboot "github.com/yasserrmd/nuraos/tests/suites/build-and-boot"
	devicespower "github.com/yasserrmd/nuraos/tests/suites/devices-power"
	e2e "github.com/yasserrmd/nuraos/tests/suites/e2e"
	loggingtime "github.com/yasserrmd/nuraos/tests/suites/logging-time"
	networkfirewall "github.com/yasserrmd/nuraos/tests/suites/network-firewall"
	performance "github.com/yasserrmd/nuraos/tests/suites/performance"
	provenancesecurity "github.com/yasserrmd/nuraos/tests/suites/provenance-security"
	providers "github.com/yasserrmd/nuraos/tests/suites/providers"
	serviceshttp "github.com/yasserrmd/nuraos/tests/suites/services-http"
	storage "github.com/yasserrmd/nuraos/tests/suites/storage"
	toolssuite "github.com/yasserrmd/nuraos/tests/suites/tools"
	updates "github.com/yasserrmd/nuraos/tests/suites/updates"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: run-suite <suite-name>\n")
		fmt.Fprintf(os.Stderr, "available: %v\n", availableSuites())
		os.Exit(1)
	}
	suiteName := os.Args[1]

	fn, ok := suiteRegistry[suiteName]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown suite %q\navailable: %v\n", suiteName, availableSuites())
		os.Exit(1)
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	rc, err := harness.NewRunContext(repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: creating run context: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[run-suite] run_id=%s  commit=%s\n", rc.RunID, rc.CommitSHA[:min(8, len(rc.CommitSHA))])

	reportDir := filepath.Join(repoRoot, "tests", "reports", suiteName)
	bundleBase := filepath.Join(repoRoot, "tests", "reports")
	ctx := context.Background()

	run, exitCode := runSuite(ctx, suiteName, fn, repoRoot, rc, bundleBase)

	if err := reporters.WriteJUnit(reportDir, run); err != nil {
		fmt.Fprintf(os.Stderr, "warning: writing JUnit report: %v\n", err)
	}
	if err := reporters.WriteJSON(reportDir, run); err != nil {
		fmt.Fprintf(os.Stderr, "warning: writing JSON report: %v\n", err)
	}

	printSummary(run)
	os.Exit(exitCode)
}

// SuiteFunc runs a suite against a booted, ready QEMUInstance and returns
// the case results.
type SuiteFunc func(ctx context.Context, inst *harness.QEMUInstance) []harness.Result

// suiteRegistry maps suite names to their runner functions.
var suiteRegistry = map[string]SuiteFunc{
	"smoke":                runSmokeFunc,
	"build-and-boot":       buildandboot.Run,
	"agent-core":           agentcore.Run,
	"providers":            providers.Run,
	"tools":                toolssuite.Run,
	"services-http":        serviceshttp.Run,
	"provenance-security":  provenancesecurity.Run,
	"storage":              storage.Run,
	"logging-time":         loggingtime.Run,
	"devices-power":        devicespower.Run,
	"network-firewall":     networkfirewall.Run,
	"updates":              updates.Run,
	"performance":          performance.Run,
	"e2e":                  e2e.Run,
}

func availableSuites() []string {
	names := make([]string, 0, len(suiteRegistry))
	for k := range suiteRegistry {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// runSuite boots QEMU, waits for readiness, runs fn, finalises results, then shuts down.
func runSuite(ctx context.Context, name string, fn SuiteFunc, repoRoot string, rc *harness.RunContext, bundleBase string) (harness.SuiteRun, int) {
	fmt.Printf("[run-suite] suite=%s  booting QEMU...\n", name)

	opts := harness.QEMUOpts{
		RepoRoot: repoRoot,
		// MemMB/CPUs: use harness defaults (512MB, 2 CPUs) to match run-qemu.sh
	}

	inst, err := harness.BootQEMU(ctx, opts)
	if err != nil {
		results := []harness.Result{bootFailResult(name, fmt.Sprintf("QEMU boot failed: %v", err))}
		harness.FinaliseResults(ctx, rc, results, nil, bundleBase)
		return harness.SuiteRun{Suite: name, Results: results}, 1
	}
	defer func() { _ = inst.Close() }()

	fmt.Printf("[run-suite] QEMU running  api=127.0.0.1:%d  waiting for /healthz...\n", inst.APIPort)

	if err := harness.WaitReady(ctx, inst, 360*time.Second); err != nil {
		results := []harness.Result{bootFailResult(name, fmt.Sprintf("guest not ready: %v", err))}
		harness.FinaliseResults(ctx, rc, results, inst, bundleBase)
		return harness.SuiteRun{Suite: name, Results: results}, 1
	}

	fmt.Printf("[run-suite] guest ready  running cases...\n")

	results := fn(ctx, inst)
	harness.FinaliseResults(ctx, rc, results, inst, bundleBase)

	run := harness.SuiteRun{Suite: name, Results: results}
	exitCode := 0
	for _, r := range results {
		if r.Status == harness.StatusFail {
			exitCode = 1
			break
		}
	}
	return run, exitCode
}

func bootFailResult(suite, msg string) harness.Result {
	return harness.Result{
		Suite:   suite,
		Case:    "boot",
		Status:  harness.StatusFail,
		Message: msg,
	}
}

// ---------------------------------------------------------------------------
// Smoke suite (built-in; exercises boot + /healthz)
// ---------------------------------------------------------------------------

func runSmokeFunc(ctx context.Context, inst *harness.QEMUInstance) []harness.Result {
	return []harness.Result{
		smokeHealthz(ctx, inst),
	}
}

func smokeHealthz(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	start := time.Now()
	code, body, err := inst.HTTP().GetBody("/healthz")
	elapsed := float64(time.Since(start).Milliseconds())

	if err != nil {
		return harness.Result{
			Suite: "smoke", Case: "healthz",
			Status: harness.StatusFail, Duration: elapsed,
			Message: fmt.Sprintf("/healthz request error: %v", err),
		}
	}
	if code != 200 {
		return harness.Result{
			Suite: "smoke", Case: "healthz",
			Status: harness.StatusFail, Duration: elapsed,
			Message: fmt.Sprintf("/healthz returned %d (want 200); body: %s", code, body),
		}
	}
	return harness.Result{
		Suite: "smoke", Case: "healthz",
		Status: harness.StatusPass, Duration: elapsed,
		Message: "/healthz returned 200",
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func printSummary(run harness.SuiteRun) {
	pass, fail, skip := 0, 0, 0
	for _, r := range run.Results {
		switch r.Status {
		case harness.StatusPass:
			pass++
			fmt.Printf("[run-suite] PASS  %s/%s\n", r.Suite, r.Case)
		case harness.StatusFail:
			fail++
			fmt.Printf("[run-suite] FAIL  %s/%s: %s\n", r.Suite, r.Case, r.Message)
		case harness.StatusSkip:
			skip++
			fmt.Printf("[run-suite] SKIP  %s/%s: %s\n", r.Suite, r.Case, r.Message)
		}
	}
	fmt.Printf("[run-suite] results: %d pass  %d fail  %d skip\n", pass, fail, skip)
}

// findRepoRoot walks up from the working directory looking for
// scripts/run-qemu.sh. Set NURA_REPO_ROOT to override.
func findRepoRoot() (string, error) {
	if r := os.Getenv("NURA_REPO_ROOT"); r != "" {
		return r, nil
	}
	d, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 12; i++ {
		if _, err := os.Stat(filepath.Join(d, "scripts", "run-qemu.sh")); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return "", fmt.Errorf("repo root not found; set NURA_REPO_ROOT or run from within the repo")
}
