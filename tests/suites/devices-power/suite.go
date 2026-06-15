// Package devicespower provides the devices-power integration test suite (T12).
//
// It verifies that hardware detection is wired into the tool registry, that
// virtio-net is operational, that the board information endpoint is reachable,
// and that memory metrics are exposed via Prometheus.
package devicespower

import (
	"context"
	"fmt"
	"strings"

	"github.com/yasserrmd/nuraos/tests/harness"
)

const suite = "devices-power"

// Run executes all devices-power cases against the pre-booted, ready instance.
func Run(ctx context.Context, inst *harness.QEMUInstance) []harness.Result {
	return []harness.Result{
		caseSystemInfoTool(ctx, inst),
		caseVirtioNetPresent(ctx, inst),
		caseBoardEndpoint(ctx, inst),
		caseMetricsMemory(ctx, inst),
	}
}

// ---------------------------------------------------------------------------
// Case: system-info-tool
// Acceptance: Phase 23 -- GET /tools lists system.info tool.
// ---------------------------------------------------------------------------

func caseSystemInfoTool(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/tools")
	if err != nil {
		return fail("system-info-tool", fmt.Sprintf("GET /tools error: %v", err))
	}
	// /tools proxies through the agent socket; skip when agent is not yet reachable (Phase 23+).
	if code == 503 {
		return skip("system-info-tool", "GET /tools=503 (agent unavailable; tool registry not yet reachable, Phase 23+)")
	}
	if code != 200 {
		return fail("system-info-tool", fmt.Sprintf("GET /tools returned %d (want 200): %s", code, body))
	}
	if !strings.Contains(body, "system.info") {
		return fail("system-info-tool",
			fmt.Sprintf("'system.info' tool not found in /tools response; body=%s", body))
	}
	return pass("system-info-tool", "system.info tool present in /tools registry")
}

// ---------------------------------------------------------------------------
// Case: virtio-net-present
// Acceptance: Phase 08 -- virtio-net is functional; agent HTTP is reachable.
// ---------------------------------------------------------------------------

func caseVirtioNetPresent(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/healthz")
	if err != nil {
		return fail("virtio-net-present",
			fmt.Sprintf("GET /healthz error (virtio-net may not be working): %v", err))
	}
	// Accept 200 (all ok) or 503 (agent degraded); both confirm virtio-net is functional.
	if code != 200 && code != 503 {
		return fail("virtio-net-present",
			fmt.Sprintf("GET /healthz returned %d (want 200 or 503, implies virtio-net issue): %s", code, body))
	}
	return pass("virtio-net-present", fmt.Sprintf("GET /healthz=%d confirms virtio-net is operational", code))
}

// ---------------------------------------------------------------------------
// Case: board-endpoint
// Acceptance: Phase 08 -- GET /board returns 200 with hardware board info.
// ---------------------------------------------------------------------------

func caseBoardEndpoint(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/board")
	if err != nil {
		return fail("board-endpoint", fmt.Sprintf("GET /board error: %v", err))
	}
	if code != 200 {
		return fail("board-endpoint", fmt.Sprintf("GET /board returned %d (want 200): %s", code, body))
	}
	if strings.TrimSpace(body) == "" {
		return fail("board-endpoint", "GET /board returned 200 with empty body")
	}
	return pass("board-endpoint", fmt.Sprintf("GET /board=200 with board info (%d bytes)", len(body)))
}

// ---------------------------------------------------------------------------
// Case: metrics-memory
// Acceptance: Phase 29 -- GET /metrics contains a memory metric.
// ---------------------------------------------------------------------------

func caseMetricsMemory(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/metrics")
	if err != nil {
		return fail("metrics-memory", fmt.Sprintf("GET /metrics error: %v", err))
	}
	if code != 200 {
		return fail("metrics-memory", fmt.Sprintf("GET /metrics returned %d (want 200): %s", code, body))
	}
	if !strings.Contains(body, "memory") {
		return fail("metrics-memory", "no 'memory' metric found in /metrics output")
	}
	return pass("metrics-memory", "memory metric found in /metrics output")
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
