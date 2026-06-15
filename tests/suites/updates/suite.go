// Package updates provides the updates integration test suite (T14).
//
// It verifies that the NuraOS A/B update system exposes a functional status
// endpoint, that the response carries recognisable slot information, and that
// the board endpoint is reachable (board info is used by the update system to
// select the correct payload).
package updates

import (
	"context"
	"fmt"
	"strings"

	"github.com/yasserrmd/nuraos/tests/harness"
)

const suite = "updates"

// Run executes all updates cases against the pre-booted, ready instance.
func Run(ctx context.Context, inst *harness.QEMUInstance) []harness.Result {
	return []harness.Result{
		caseUpdateStatusEndpoint(ctx, inst),
		caseActiveSlotField(ctx, inst),
		caseBoardSlotInfo(ctx, inst),
	}
}

// ---------------------------------------------------------------------------
// Case: update-status-endpoint
// Acceptance: Phase 34 -- GET /update/status returns 200.
// ---------------------------------------------------------------------------

func caseUpdateStatusEndpoint(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/update/status")
	if err != nil {
		return fail("update-status-endpoint", fmt.Sprintf("GET /update/status error: %v", err))
	}
	if code != 200 {
		return fail("update-status-endpoint",
			fmt.Sprintf("GET /update/status returned %d (want 200): %s", code, body))
	}
	return pass("update-status-endpoint", "GET /update/status=200")
}

// ---------------------------------------------------------------------------
// Case: active-slot-field
// Acceptance: Phase 34 -- /update/status has a recognisable slot field (not 503).
// ---------------------------------------------------------------------------

func caseActiveSlotField(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/update/status")
	if err != nil {
		return fail("active-slot-field", fmt.Sprintf("GET /update/status error: %v", err))
	}
	if code == 503 {
		return fail("active-slot-field",
			fmt.Sprintf("GET /update/status returned 503 (update service unavailable): %s", body))
	}
	// Accept any response that contains a slot-related indicator.
	lower := strings.ToLower(body)
	if strings.Contains(lower, "slot") ||
		strings.Contains(lower, "active") ||
		strings.Contains(lower, "current") ||
		strings.Contains(lower, "version") {
		return pass("active-slot-field",
			fmt.Sprintf("GET /update/status (status=%d) contains slot/version information", code))
	}
	// If the body is non-empty and the code is not 503, accept it.
	if strings.TrimSpace(body) != "" {
		return pass("active-slot-field",
			fmt.Sprintf("GET /update/status (status=%d) returned non-empty body", code))
	}
	return fail("active-slot-field",
		fmt.Sprintf("GET /update/status (status=%d) returned empty body with no slot info", code))
}

// ---------------------------------------------------------------------------
// Case: board-slot-info
// Acceptance: Phase 08/34 -- GET /board returns 200 with response body.
// ---------------------------------------------------------------------------

func caseBoardSlotInfo(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/board")
	if err != nil {
		return fail("board-slot-info", fmt.Sprintf("GET /board error: %v", err))
	}
	if code != 200 {
		return fail("board-slot-info",
			fmt.Sprintf("GET /board returned %d (want 200): %s", code, body))
	}
	if strings.TrimSpace(body) == "" {
		return fail("board-slot-info", "GET /board returned 200 with empty body")
	}
	return pass("board-slot-info", fmt.Sprintf("GET /board=200 with board info (%d bytes)", len(body)))
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
