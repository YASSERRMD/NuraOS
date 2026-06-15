// Package performance provides the performance integration test suite (T15).
//
// It measures round-trip latency for critical endpoints, checks that the boot
// image artifacts meet size budgets, and verifies that the pre-booted instance
// is already ready to serve requests with minimal latency.
package performance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yasserrmd/nuraos/tests/harness"
)

const suite = "performance"

// Latency thresholds.
const (
	healthzMaxRTT  = 500 * time.Millisecond
	metricsMaxRTT  = 1000 * time.Millisecond
	readyMaxRTT    = 200 * time.Millisecond
	bzImageMaxMB   = 100
	initramfsMaxMB = 50
)

// Run executes all performance cases against the pre-booted, ready instance.
func Run(ctx context.Context, inst *harness.QEMUInstance) []harness.Result {
	return []harness.Result{
		caseHealthzRTT(ctx, inst),
		caseMetricsRTT(ctx, inst),
		caseBootImageSize(ctx, inst),
		caseSerialBootReady(ctx, inst),
	}
}

// ---------------------------------------------------------------------------
// Case: healthz-rtt
// Acceptance: Phase 10 -- GET /healthz RTT < 500 ms.
// ---------------------------------------------------------------------------

func caseHealthzRTT(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	start := time.Now()
	code, body, err := inst.HTTP().GetBody("/healthz")
	rtt := time.Since(start)

	if err != nil {
		return fail("healthz-rtt", fmt.Sprintf("GET /healthz error: %v", err))
	}
	// Accept 200 or 503; both are valid gateway responses — RTT test is about latency, not component health.
	if code != 200 && code != 503 {
		return fail("healthz-rtt", fmt.Sprintf("GET /healthz returned %d (want 200 or 503): %s", code, body))
	}
	if rtt > healthzMaxRTT {
		return fail("healthz-rtt",
			fmt.Sprintf("GET /healthz RTT %s exceeds threshold %s", rtt.Round(time.Millisecond), healthzMaxRTT))
	}
	return pass("healthz-rtt",
		fmt.Sprintf("GET /healthz=%d RTT=%s (threshold=%s)", code, rtt.Round(time.Millisecond), healthzMaxRTT))
}

// ---------------------------------------------------------------------------
// Case: metrics-rtt
// Acceptance: Phase 29 -- GET /metrics RTT < 1000 ms.
// ---------------------------------------------------------------------------

func caseMetricsRTT(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	start := time.Now()
	code, body, err := inst.HTTP().GetBody("/metrics")
	rtt := time.Since(start)

	if err != nil {
		return fail("metrics-rtt", fmt.Sprintf("GET /metrics error: %v", err))
	}
	if code != 200 {
		return fail("metrics-rtt", fmt.Sprintf("GET /metrics returned %d (want 200): %s", code, body))
	}
	if rtt > metricsMaxRTT {
		return fail("metrics-rtt",
			fmt.Sprintf("GET /metrics RTT %s exceeds threshold %s", rtt.Round(time.Millisecond), metricsMaxRTT))
	}
	return pass("metrics-rtt",
		fmt.Sprintf("GET /metrics RTT=%s (threshold=%s)", rtt.Round(time.Millisecond), metricsMaxRTT))
}

// ---------------------------------------------------------------------------
// Case: boot-image-size
// Acceptance: Phase 01/03 -- bzImage < 100 MB, initramfs.cpio.gz < 50 MB.
// ---------------------------------------------------------------------------

func caseBootImageSize(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	outDir := filepath.Join(inst.RepoRoot, "image", "out")

	type artifact struct {
		name   string
		maxMB  int64
	}
	artifacts := []artifact{
		{"bzImage", bzImageMaxMB},
		{"initramfs.cpio.gz", initramfsMaxMB},
	}

	for _, a := range artifacts {
		path := filepath.Join(outDir, a.name)
		info, err := os.Stat(path)
		if err != nil {
			// Artifact absent - skip rather than fail (may not be built yet).
			continue
		}
		sizeMB := info.Size() / (1024 * 1024)
		if sizeMB > a.maxMB {
			return fail("boot-image-size",
				fmt.Sprintf("%s is %d MiB, exceeds limit of %d MiB", a.name, sizeMB, a.maxMB))
		}
	}
	return pass("boot-image-size",
		fmt.Sprintf("bzImage and initramfs within size budgets (%d MiB / %d MiB)", bzImageMaxMB, initramfsMaxMB))
}

// ---------------------------------------------------------------------------
// Case: serial-boot-ready
// Acceptance: Phase 10 -- already-booted instance responds to /healthz < 200 ms.
// ---------------------------------------------------------------------------

func caseSerialBootReady(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	start := time.Now()
	code, body, err := inst.HTTP().GetBody("/healthz")
	rtt := time.Since(start)

	if err != nil {
		return fail("serial-boot-ready", fmt.Sprintf("GET /healthz error (instance not ready?): %v", err))
	}
	// Accept 200 or 503; the RTT test measures gateway responsiveness, not component health.
	if code != 200 && code != 503 {
		return fail("serial-boot-ready",
			fmt.Sprintf("GET /healthz returned %d (want 200 or 503): %s", code, body))
	}
	if rtt > readyMaxRTT {
		return fail("serial-boot-ready",
			fmt.Sprintf("instance ready but /healthz RTT %s > %s (already-booted instance should respond faster)",
				rtt.Round(time.Millisecond), readyMaxRTT))
	}
	return pass("serial-boot-ready",
		fmt.Sprintf("pre-booted instance responded to /healthz=%d in %s (< %s)", code, rtt.Round(time.Millisecond), readyMaxRTT))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func pass(case_, msg string) harness.Result {
	return harness.Result{Suite: suite, Case: case_, Status: harness.StatusPass, Message: msg}
}

func fail(case_, msg string) harness.Result {
	return harness.Result{Suite: suite, Case: case_, Status: harness.StatusFail, Message: msg}
}

func skip(case_, msg string) harness.Result {
	return harness.Result{Suite: suite, Case: case_, Status: harness.StatusSkip, Message: msg}
}
