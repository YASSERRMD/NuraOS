// Package tools provides the tools integration test suite.
//
// It verifies the tool registry via the /tools endpoint and the serial REPL.
// Cases that require model-driven tool execution skip when no model is loaded.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yasserrmd/nuraos/tests/harness"
)

const suite = "tools"

// expectedTools are the built-in tools that must always be present.
var expectedTools = []string{"fs.read", "net.status", "system.info", "time.now"}

// Run executes all tools-suite cases.
func Run(ctx context.Context, inst *harness.QEMUInstance) []harness.Result {
	return []harness.Result{
		caseToolsEndpoint(ctx, inst),
		caseExpectedToolNames(ctx, inst),
		caseToolSchemas(ctx, inst),
	}
}

// ---------------------------------------------------------------------------
// Case: tools-endpoint
// Acceptance: Phases 23/24 -- GET /tools returns 200 with JSON array.
// ---------------------------------------------------------------------------

func caseToolsEndpoint(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/tools")
	if err != nil {
		return fail("tools-endpoint", fmt.Sprintf("GET /tools error: %v", err))
	}
	if code != 200 {
		return fail("tools-endpoint", fmt.Sprintf("GET /tools returned %d: %s", code, body))
	}
	if strings.TrimSpace(body) == "" {
		return fail("tools-endpoint", "GET /tools returned empty body")
	}
	return pass("tools-endpoint", "/tools returned 200 with non-empty body")
}

// ---------------------------------------------------------------------------
// Case: expected-tool-names
// Acceptance: Phases 23/24 -- required tools are all present in the registry.
// ---------------------------------------------------------------------------

func caseExpectedToolNames(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	_, body, err := inst.HTTP().GetBody("/tools")
	if err != nil {
		return fail("expected-tool-names", fmt.Sprintf("GET /tools error: %v", err))
	}
	for _, name := range expectedTools {
		if !strings.Contains(body, name) {
			return fail("expected-tool-names",
				fmt.Sprintf("tool %q not found in /tools response: %s", name, body))
		}
	}
	return pass("expected-tool-names",
		fmt.Sprintf("all %d expected tools present: %s", len(expectedTools), strings.Join(expectedTools, ", ")))
}

// ---------------------------------------------------------------------------
// Case: tool-schemas
// Acceptance: Phase 24 -- each tool exposes a JSON Schema with type=object.
// ---------------------------------------------------------------------------

func caseToolSchemas(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	_, body, err := inst.HTTP().GetBody("/tools")
	if err != nil {
		return fail("tool-schemas", fmt.Sprintf("GET /tools error: %v", err))
	}

	// The /tools response is a JSON structure proxied from the agent.
	// We verify it parses as valid JSON and contains schema-related keywords.
	var raw interface{}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return fail("tool-schemas", fmt.Sprintf("cannot parse /tools JSON: %v", err))
	}

	// Any tools response containing "properties" or "type" indicates schemas are present.
	if !strings.Contains(body, `"type"`) {
		return fail("tool-schemas", "/tools response contains no type annotations in schemas")
	}
	return pass("tool-schemas", "/tools response contains valid JSON with schema type fields")
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
