// Package loggingtime provides the logging-time integration test suite (T11).
//
// It verifies that NuraOS emits structured, timestamped log output via both
// the serial console and the Prometheus metrics endpoint, enabling reliable
// post-mortem analysis and time-series correlation.
package loggingtime

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/yasserrmd/nuraos/tests/harness"
)

const suite = "logging-time"

// Run executes all logging-time cases against the pre-booted, ready instance.
func Run(ctx context.Context, inst *harness.QEMUInstance) []harness.Result {
	return []harness.Result{
		caseSerialLogTimestamps(ctx, inst),
		caseLogLevelsPresent(ctx, inst),
		caseMetricsUptime(ctx, inst),
		caseStructuredFields(ctx, inst),
	}
}

// ---------------------------------------------------------------------------
// Case: serial-log-timestamps
// Acceptance: Phase 13 -- serial log contains ISO 8601 timestamps.
// ---------------------------------------------------------------------------

// iso8601TimePattern matches the time component of an ISO 8601 timestamp,
// e.g. "T12:34:56" in "2024-01-01T12:34:56Z".
var iso8601TimePattern = regexp.MustCompile(`T\d\d:\d\d:\d\d`)

func caseSerialLogTimestamps(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	data, err := os.ReadFile(inst.SerialLogPath)
	if err != nil {
		// Fall back to the live serial buffer.
		snap := inst.Serial().Snapshot()
		if len(snap) == 0 {
			return fail("serial-log-timestamps", fmt.Sprintf("serial log unreadable and buffer empty: %v", err))
		}
		data = snap
	}
	if iso8601TimePattern.Match(data) {
		return pass("serial-log-timestamps", "ISO 8601 timestamp pattern (T##:##:##) found in serial log")
	}
	return fail("serial-log-timestamps", "no ISO 8601 timestamp pattern found in serial log")
}

// ---------------------------------------------------------------------------
// Case: log-levels-present
// Acceptance: Phase 13 -- serial log contains INFO level markers.
// ---------------------------------------------------------------------------

func caseLogLevelsPresent(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	// Wait up to 10 s in case the log is still accumulating at suite start.
	if err := inst.Serial().WaitForPattern("INFO", 10*time.Second); err != nil {
		// Also accept lowercase or JSON-encoded level field.
		snap := inst.Serial().Snapshot()
		if strings.Contains(string(snap), "info") || strings.Contains(string(snap), `"level"`) {
			return pass("log-levels-present", "log level marker ('info' or JSON level) found in serial output")
		}
		return fail("log-levels-present",
			fmt.Sprintf("no INFO level markers found in serial log within 10s: %v", err))
	}
	return pass("log-levels-present", "INFO level marker found in serial log")
}

// ---------------------------------------------------------------------------
// Case: metrics-uptime
// Acceptance: Phase 29 -- GET /metrics contains uptime or start_time metric.
// ---------------------------------------------------------------------------

func caseMetricsUptime(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/metrics")
	if err != nil {
		return fail("metrics-uptime", fmt.Sprintf("GET /metrics error: %v", err))
	}
	if code != 200 {
		return fail("metrics-uptime", fmt.Sprintf("GET /metrics returned %d (want 200): %s", code, body))
	}
	if strings.Contains(body, "uptime") || strings.Contains(body, "start_time") {
		return pass("metrics-uptime", "uptime/start_time metric found in /metrics output")
	}
	return fail("metrics-uptime", "no 'uptime' or 'start_time' metric found in /metrics output")
}

// ---------------------------------------------------------------------------
// Case: structured-fields
// Acceptance: Phase 13 -- serial log has key=value or JSON structured fields.
// ---------------------------------------------------------------------------

// kvPattern matches Logfmt-style key=value pairs (e.g. level=info, msg=starting).
var kvPattern = regexp.MustCompile(`\w+=\S+`)

func caseStructuredFields(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	data, err := os.ReadFile(inst.SerialLogPath)
	if err != nil {
		snap := inst.Serial().Snapshot()
		if len(snap) == 0 {
			return fail("structured-fields", fmt.Sprintf("serial log unreadable and buffer empty: %v", err))
		}
		data = snap
	}
	content := string(data)
	// Accept either Logfmt (level=info) or JSON ({"level":"info",...}).
	if kvPattern.MatchString(content) || strings.Contains(content, `"level"`) {
		return pass("structured-fields", "structured log fields (key=value or JSON) found in serial log")
	}
	return fail("structured-fields", "no structured log fields found in serial log")
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
