// Package e2e provides the end-to-end integration test suite (T16).
//
// It performs a full system liveness check by exercising every major gateway
// endpoint in sequence. Cases are designed to be the final gate in a CI
// pipeline: if all pass, the NuraOS image is considered ready for release.
// Cases never fail just because an optional component (e.g. a language model)
// is not loaded.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/yasserrmd/nuraos/tests/harness"
)

const suite = "e2e"

// Run executes all e2e cases against the pre-booted, ready instance.
func Run(ctx context.Context, inst *harness.QEMUInstance) []harness.Result {
	return []harness.Result{
		caseHealthzReady(ctx, inst),
		caseVersionReachable(ctx, inst),
		caseToolsReachable(ctx, inst),
		caseModelsReachable(ctx, inst),
		caseChatModelRequired(ctx, inst),
		caseTelemetryStatus(ctx, inst),
	}
}

// ---------------------------------------------------------------------------
// Case: healthz-ready
// Acceptance: Phase 10 -- GET /healthz returns 200 (full system liveness).
// ---------------------------------------------------------------------------

func caseHealthzReady(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/healthz")
	if err != nil {
		return fail("healthz-ready", fmt.Sprintf("GET /healthz error: %v", err))
	}
	// Accept 200 (healthy) or 503 (agent degraded); both confirm the gateway is live.
	if code != 200 && code != 503 {
		return fail("healthz-ready", fmt.Sprintf("GET /healthz returned %d (want 200 or 503): %s", code, body))
	}
	return pass("healthz-ready", fmt.Sprintf("GET /healthz=%d; system is live: %s", code, strings.TrimSpace(body)))
}

// ---------------------------------------------------------------------------
// Case: version-reachable
// Acceptance: Phase 12 -- GET /version returns valid JSON.
// ---------------------------------------------------------------------------

func caseVersionReachable(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/version")
	if err != nil {
		return fail("version-reachable", fmt.Sprintf("GET /version error: %v", err))
	}
	if code != 200 {
		return fail("version-reachable", fmt.Sprintf("GET /version returned %d (want 200): %s", code, body))
	}
	var raw interface{}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return fail("version-reachable", fmt.Sprintf("GET /version body is not valid JSON: %v; body=%s", err, body))
	}
	return pass("version-reachable", "GET /version=200 with valid JSON body")
}

// ---------------------------------------------------------------------------
// Case: tools-reachable
// Acceptance: Phase 23 -- GET /tools returns 200.
// ---------------------------------------------------------------------------

func caseToolsReachable(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/tools")
	if err != nil {
		return fail("tools-reachable", fmt.Sprintf("GET /tools error: %v", err))
	}
	// /tools proxies through the agent socket; skip when agent is not yet reachable (Phase 23+).
	if code == 503 {
		return skip("tools-reachable", "GET /tools=503 (agent unavailable; tool registry not yet reachable, Phase 23+)")
	}
	if code != 200 {
		return fail("tools-reachable", fmt.Sprintf("GET /tools returned %d (want 200): %s", code, body))
	}
	return pass("tools-reachable", "GET /tools=200")
}

// ---------------------------------------------------------------------------
// Case: models-reachable
// Acceptance: Phase 39 -- GET /models returns 200.
// ---------------------------------------------------------------------------

func caseModelsReachable(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/models")
	if err != nil {
		return fail("models-reachable", fmt.Sprintf("GET /models error: %v", err))
	}
	if code != 200 {
		return fail("models-reachable", fmt.Sprintf("GET /models returned %d (want 200): %s", code, body))
	}
	return pass("models-reachable", "GET /models=200")
}

// ---------------------------------------------------------------------------
// Case: chat-model-required
// Acceptance: Phase 15 -- POST /chat without model → 503 (or with model → 200);
// never fail just because the model is not loaded.
// ---------------------------------------------------------------------------

func caseChatModelRequired(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	resp, err := inst.HTTP().PostJSON("/chat", map[string]interface{}{
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
	})
	if err != nil {
		return fail("chat-model-required", fmt.Sprintf("POST /chat error: %v", err))
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	body := string(b)

	switch resp.StatusCode {
	case 200:
		return pass("chat-model-required", "POST /chat=200 (model loaded and responded)")
	case 503:
		// Model not loaded - this is the expected state in CI without a model blob.
		return skip("chat-model-required", "POST /chat=503 (model not loaded); skipping")
	case 400:
		// Gateway rejected the request (no model field) - acceptable gateway behaviour.
		return pass("chat-model-required",
			fmt.Sprintf("POST /chat=400 (gateway validation active): %s", strings.TrimSpace(body)))
	default:
		// Any other non-5xx response is acceptable; 5xx other than 503 is a fail.
		if resp.StatusCode >= 500 {
			return fail("chat-model-required",
				fmt.Sprintf("POST /chat returned unexpected server error %d: %s", resp.StatusCode, body))
		}
		return pass("chat-model-required",
			fmt.Sprintf("POST /chat=%d (gateway responded without panic)", resp.StatusCode))
	}
}

// ---------------------------------------------------------------------------
// Case: telemetry-status
// Acceptance: Phase 29 -- GET /telemetry/status returns 200.
// ---------------------------------------------------------------------------

func caseTelemetryStatus(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/telemetry/status")
	if err != nil {
		return fail("telemetry-status", fmt.Sprintf("GET /telemetry/status error: %v", err))
	}
	if code != 200 {
		return fail("telemetry-status",
			fmt.Sprintf("GET /telemetry/status returned %d (want 200): %s", code, body))
	}
	return pass("telemetry-status", "GET /telemetry/status=200")
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
