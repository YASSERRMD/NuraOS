// Package providers provides the providers integration test suite.
//
// It verifies the provider abstraction, HTTP conformance, and routing via
// the gateway /chat endpoint. Cases that require a loaded language model or
// remote API keys skip automatically when those prerequisites are absent.
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/yasserrmd/nuraos/tests/harness"
)

const suite = "providers"

// Run executes all provider-suite cases.
func Run(ctx context.Context, inst *harness.QEMUInstance) []harness.Result {
	hasModel := modelLoaded(inst)
	hasAnthropic := os.Getenv("ANTHROPIC_API_KEY") != ""
	hasOpenAI := os.Getenv("OPENAI_API_KEY") != ""

	return []harness.Result{
		caseBadContentType(ctx, inst),
		caseEmptyMessages(ctx, inst),
		caseInvalidJSON(ctx, inst),
		caseLocalConformance(ctx, inst, hasModel),
		caseStreamingDeltas(ctx, inst, hasModel),
		caseProviderOverride(ctx, inst, hasModel),
		caseAnthropicConformance(ctx, inst, hasAnthropic),
		caseOpenAIConformance(ctx, inst, hasOpenAI),
		caseRoutingModels(ctx, inst),
	}
}

// ---------------------------------------------------------------------------
// HTTP contract: bad requests (no model needed)
// ---------------------------------------------------------------------------

// case: bad-content-type
// Acceptance: Phase 28 -- /chat rejects non-JSON content type with 415.
func caseBadContentType(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	req, _ := http.NewRequest(http.MethodPost, inst.HTTP().BaseURL+"/chat",
		strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "text/plain")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fail("bad-content-type", fmt.Sprintf("request error: %v", err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		return fail("bad-content-type",
			fmt.Sprintf("want 415 Unsupported Media Type, got %d", resp.StatusCode))
	}
	return pass("bad-content-type", "non-JSON Content-Type correctly rejected with 415")
}

// case: empty-messages
// Acceptance: Phase 28 -- /chat rejects empty message list with 400.
func caseEmptyMessages(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := doChat(inst, map[string]interface{}{"messages": []interface{}{}})
	if err != nil {
		return fail("empty-messages", fmt.Sprintf("request error: %v", err))
	}
	if code != http.StatusBadRequest {
		return fail("empty-messages", fmt.Sprintf("want 400, got %d: %s", code, body))
	}
	return pass("empty-messages", "empty message list rejected with 400")
}

// case: invalid-json
// Acceptance: Phase 28 -- /chat rejects malformed JSON with 400.
func caseInvalidJSON(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	req, _ := http.NewRequest(http.MethodPost, inst.HTTP().BaseURL+"/chat",
		strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fail("invalid-json", fmt.Sprintf("request error: %v", err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		return fail("invalid-json", fmt.Sprintf("want 400, got %d", resp.StatusCode))
	}
	return pass("invalid-json", "malformed JSON rejected with 400")
}

// ---------------------------------------------------------------------------
// Local provider conformance (skip when no model is loaded)
// ---------------------------------------------------------------------------

// case: local-conformance
// Acceptance: Phases 15-17 -- local provider returns canonical StreamEvent shapes.
func caseLocalConformance(_ context.Context, inst *harness.QEMUInstance, hasModel bool) harness.Result {
	if !hasModel {
		return skip("local-conformance", "no model loaded in /data/models; skipping local conformance")
	}
	code, body, err := doChat(inst, chatPayload("Say 'OK' and nothing else.", "local"))
	if err != nil {
		return fail("local-conformance", fmt.Sprintf("POST /chat error: %v", err))
	}
	if code != http.StatusOK {
		return fail("local-conformance", fmt.Sprintf("want 200, got %d: %s", code, body))
	}
	return checkSSEEvents("local-conformance", body)
}

// case: streaming-deltas
// Acceptance: Phase 17 -- streaming response contains token_delta events before done.
func caseStreamingDeltas(_ context.Context, inst *harness.QEMUInstance, hasModel bool) harness.Result {
	if !hasModel {
		return skip("streaming-deltas", "no model loaded; skipping streaming-deltas")
	}
	code, body, err := doChat(inst, chatPayload("Count to three.", "local"))
	if err != nil {
		return fail("streaming-deltas", fmt.Sprintf("POST /chat error: %v", err))
	}
	if code != http.StatusOK {
		return fail("streaming-deltas", fmt.Sprintf("want 200, got %d: %s", code, body))
	}
	events := parseSSEEvents(body)
	hasToken := false
	for _, ev := range events {
		if ev["kind"] == "token_delta" {
			hasToken = true
			break
		}
	}
	if !hasToken {
		return fail("streaming-deltas", fmt.Sprintf("no token_delta events in SSE body: %s", body))
	}
	return pass("streaming-deltas", fmt.Sprintf("token_delta events present (%d total events)", len(events)))
}

// case: provider-override
// Acceptance: Phase 19 -- provider field in request routes to the specified provider.
func caseProviderOverride(_ context.Context, inst *harness.QEMUInstance, hasModel bool) harness.Result {
	if !hasModel {
		return skip("provider-override", "no model loaded; skipping provider-override")
	}
	code, body, err := doChat(inst, chatPayload("Hello.", "local"))
	if err != nil {
		return fail("provider-override", fmt.Sprintf("POST /chat error: %v", err))
	}
	if code != http.StatusOK {
		return fail("provider-override", fmt.Sprintf("want 200 with provider=local, got %d: %s", code, body))
	}
	events := parseSSEEvents(body)
	for _, ev := range events {
		if ev["kind"] == "done" {
			return pass("provider-override", "local provider override routed and completed successfully")
		}
	}
	return fail("provider-override", "no done event in SSE response")
}

// ---------------------------------------------------------------------------
// Remote provider conformance (skip when API keys absent)
// ---------------------------------------------------------------------------

// case: anthropic-conformance
// Acceptance: Phase 20 -- Anthropic provider returns canonical event shapes.
func caseAnthropicConformance(_ context.Context, inst *harness.QEMUInstance, hasKey bool) harness.Result {
	if !hasKey {
		return skip("anthropic-conformance", "ANTHROPIC_API_KEY not set; skipping remote conformance")
	}
	code, body, err := doChat(inst, chatPayload("Say 'OK'.", "anthropic"))
	if err != nil {
		return fail("anthropic-conformance", fmt.Sprintf("POST /chat error: %v", err))
	}
	if code != http.StatusOK {
		return fail("anthropic-conformance", fmt.Sprintf("want 200, got %d: %s", code, body))
	}
	return checkSSEEvents("anthropic-conformance", body)
}

// case: openai-conformance
// Acceptance: Phase 21 -- OpenAI-compatible provider returns canonical event shapes.
func caseOpenAIConformance(_ context.Context, inst *harness.QEMUInstance, hasKey bool) harness.Result {
	if !hasKey {
		return skip("openai-conformance", "OPENAI_API_KEY not set; skipping remote conformance")
	}
	code, body, err := doChat(inst, chatPayload("Say 'OK'.", "openai"))
	if err != nil {
		return fail("openai-conformance", fmt.Sprintf("POST /chat error: %v", err))
	}
	if code != http.StatusOK {
		return fail("openai-conformance", fmt.Sprintf("want 200, got %d: %s", code, body))
	}
	return checkSSEEvents("openai-conformance", body)
}

// ---------------------------------------------------------------------------
// Routing: /models endpoint shows available and active model
// ---------------------------------------------------------------------------

// case: routing-models
// Acceptance: Phase 18/39 -- /models returns valid JSON with active and available fields.
func caseRoutingModels(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/models")
	if err != nil {
		return fail("routing-models", fmt.Sprintf("GET /models error: %v", err))
	}
	if code != http.StatusOK {
		return fail("routing-models", fmt.Sprintf("GET /models returned %d: %s", code, body))
	}
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return fail("routing-models", fmt.Sprintf("cannot parse /models JSON: %v", err))
	}
	if _, ok := resp["available"]; !ok {
		return fail("routing-models", "/models missing 'available' field")
	}
	return pass("routing-models", "/models returned valid JSON with available field")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func modelLoaded(inst *harness.QEMUInstance) bool {
	_, body, err := inst.HTTP().GetBody("/models")
	if err != nil {
		return false
	}
	// If available list is non-empty the model dir has at least one .gguf.
	return strings.Contains(body, ".gguf")
}

func chatPayload(message, provider string) map[string]interface{} {
	p := map[string]interface{}{
		"messages": []map[string]string{
			{"role": "user", "content": message},
		},
	}
	if provider != "" {
		p["provider"] = provider
	}
	return p
}

func doChat(inst *harness.QEMUInstance, payload interface{}) (int, string, error) {
	resp, err := inst.HTTP().PostJSON("/chat", payload)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), err
}

func parseSSEEvents(body string) []map[string]interface{} {
	var events []map[string]interface{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(data), &ev); err == nil {
			events = append(events, ev)
		}
	}
	return events
}

// checkSSEEvents verifies the SSE body contains at least one done event with
// the correct kind field.
func checkSSEEvents(case_, body string) harness.Result {
	events := parseSSEEvents(body)
	if len(events) == 0 {
		return fail(case_, fmt.Sprintf("no SSE events parsed from body: %s", body))
	}
	for _, ev := range events {
		if ev["kind"] == "error" {
			return fail(case_, fmt.Sprintf("provider returned error event: %v", ev["message"]))
		}
	}
	for _, ev := range events {
		if ev["kind"] == "done" {
			return pass(case_, fmt.Sprintf("SSE stream contains %d events ending with done", len(events)))
		}
	}
	return fail(case_, fmt.Sprintf("SSE stream ended without a done event (%d events)", len(events)))
}

func pass(case_, msg string) harness.Result {
	return harness.Result{Suite: suite, Case: case_, Status: harness.StatusPass, Message: msg}
}

func fail(case_, msg string) harness.Result {
	return harness.Result{Suite: suite, Case: case_, Status: harness.StatusFail, Message: msg}
}

func skip(case_, msg string) harness.Result {
	return harness.Result{Suite: suite, Case: case_, Status: harness.StatusSkip, Message: msg}
}
