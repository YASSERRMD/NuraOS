package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/agent"
)

// TestMetricsStoreCounters verifies that each increment method advances the
// correct atomic counter.
func TestMetricsStoreCounters(t *testing.T) {
	m := newMetricsStore()
	m.incRequest(epChat)
	m.incRequest(epChat)
	m.incRequest(epTools)
	m.incRateLimited()
	m.incConcurrencyBusy()
	m.recordChatLatency(100 * time.Millisecond)

	if got := m.reqTotal[epChat].Load(); got != 2 {
		t.Errorf("chat requests: want 2, got %d", got)
	}
	if got := m.reqTotal[epTools].Load(); got != 1 {
		t.Errorf("tools requests: want 1, got %d", got)
	}
	if got := m.rateLimited.Load(); got != 1 {
		t.Errorf("rateLimited: want 1, got %d", got)
	}
	if got := m.concurrencyBusy.Load(); got != 1 {
		t.Errorf("concurrencyBusy: want 1, got %d", got)
	}
	if got := m.chatLatCount.Load(); got != 1 {
		t.Errorf("chatLatCount: want 1, got %d", got)
	}
	if got := m.chatLatUS.Load(); got <= 0 {
		t.Errorf("chatLatUS: want >0, got %d", got)
	}
}

// TestMetricsStoreNilSafe verifies that all increment methods and WriteTo are
// no-ops on a nil receiver rather than panicking.
func TestMetricsStoreNilSafe(t *testing.T) {
	var m *MetricsStore
	m.incRequest(epChat)
	m.incRateLimited()
	m.incConcurrencyBusy()
	m.recordChatLatency(time.Second)
	if got := m.uptimeSeconds(); got != 0 {
		t.Errorf("nil uptimeSeconds: want 0, got %d", got)
	}
	var sb strings.Builder
	m.WriteTo(&sb, nil) // must not panic
	if !strings.Contains(sb.String(), "unavailable") {
		t.Errorf("nil WriteTo: want 'unavailable' comment, got %q", sb.String())
	}
}

// TestMetricsWriteToGatewayCounters verifies that gateway counters appear in
// Prometheus text output and that format rules are followed (HELP + TYPE before
// each metric family, counter names present).
func TestMetricsWriteToGatewayCounters(t *testing.T) {
	m := newMetricsStore()
	m.incRequest(epChat)
	m.incRequest(epChat)
	m.incRateLimited()
	m.incConcurrencyBusy()

	var sb strings.Builder
	m.WriteTo(&sb, nil)
	output := sb.String()

	mustContain := []string{
		"# HELP nura_gateway_uptime_seconds",
		"# TYPE nura_gateway_uptime_seconds gauge",
		"nura_gateway_uptime_seconds",
		`nura_gateway_requests_total{endpoint="chat"} 2`,
		`nura_gateway_requests_total{endpoint="tools"} 0`,
		"nura_gateway_rate_limited_total 1",
		"nura_gateway_concurrency_rejected_total 1",
		"nura_gateway_chat_latency_microseconds_total",
		"nura_gateway_chat_requests_completed_total",
		"process_resident_memory_bytes",
	}
	for _, want := range mustContain {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q", want)
		}
	}
	if strings.Contains(output, "nura_agent_") {
		t.Error("agent metrics should be absent when agentMet is nil")
	}
}

// TestMetricsWriteToAgentCounters verifies that agent metrics appear when an
// AgentMetrics pointer is provided.
func TestMetricsWriteToAgentCounters(t *testing.T) {
	m := newMetricsStore()
	agentMet := &agent.AgentMetrics{
		TokensIn:         1000,
		TokensOut:        500,
		TurnsTotal:       3,
		UptimeSeconds:    120,
		ToolCallsTotal:   map[string]int64{"fs.read": 5},
		ProviderRequests: map[string]int64{"local": 3},
	}

	var sb strings.Builder
	m.WriteTo(&sb, agentMet)
	output := sb.String()

	mustContain := []string{
		"nura_agent_tokens_in_total 1000",
		"nura_agent_tokens_out_total 500",
		"nura_agent_turns_total 3",
		"nura_agent_uptime_seconds 120",
		`nura_agent_tool_calls_total{tool="fs.read"} 5`,
		`nura_agent_provider_requests_total{provider="local"} 3`,
	}
	for _, want := range mustContain {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

// TestMetricsEndpointOK verifies GET /metrics returns 200 with Prometheus
// content-type and gateway counter lines; agent metrics are included when the
// fake agent exposes /metrics.
func TestMetricsEndpointOK(t *testing.T) {
	socketPath, stop := startFakeAgent(t, fakeAgentMux())
	defer stop()

	store := newMetricsStore()
	store.incRequest(epChat)
	h := &handlers{agentClient: agent.New(socketPath, 500*time.Millisecond), store: store}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	h.metricsHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%s", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type: want text/plain, got %q", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "nura_gateway_requests_total") {
		t.Errorf("body missing nura_gateway_requests_total")
	}
	// Fake agent exposes /metrics; agent counters should be present.
	if !strings.Contains(body, "nura_agent_tokens_in_total") {
		t.Errorf("body missing nura_agent_tokens_in_total (agent should be reachable)")
	}
}

// TestMetricsEndpointAgentUnavailable verifies that /metrics still returns 200
// with gateway counters when the agent socket is unreachable.
func TestMetricsEndpointAgentUnavailable(t *testing.T) {
	store := newMetricsStore()
	h := &handlers{agentClient: agent.New("/nonexistent-p31.sock", 50*time.Millisecond), store: store}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	h.metricsHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("want 200 even when agent down, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "nura_gateway_uptime_seconds") {
		t.Errorf("body missing gateway metrics: %s", body)
	}
	if strings.Contains(body, "nura_agent_tokens_in_total") {
		t.Error("agent metrics must be absent when agent is unreachable")
	}
}

// TestStatusEndpointOK verifies GET /status returns 200 with overall=ok when
// the agent reports healthy.
func TestStatusEndpointOK(t *testing.T) {
	socketPath, stop := startFakeAgent(t, fakeAgentMux())
	defer stop()

	store := newMetricsStore()
	h := &handlers{agentClient: agent.New(socketPath, 500*time.Millisecond), store: store}

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rr := httptest.NewRecorder()
	h.statusHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%s", rr.Code, rr.Body.String())
	}
	var resp agent.StatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Overall != "ok" {
		t.Errorf("want overall=ok, got %q", resp.Overall)
	}
	if len(resp.Components) < 2 {
		t.Errorf("want at least 2 components (gateway + agent), got %d", len(resp.Components))
	}
	if resp.Uptime < 0 {
		t.Errorf("uptime must be non-negative, got %d", resp.Uptime)
	}
}

// TestStatusEndpointDegraded verifies GET /status returns 503 with
// overall=degraded when the agent socket is unreachable.
func TestStatusEndpointDegraded(t *testing.T) {
	store := newMetricsStore()
	h := &handlers{agentClient: agent.New("/nonexistent-p31.sock", 50*time.Millisecond), store: store}

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rr := httptest.NewRecorder()
	h.statusHandler(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", rr.Code)
	}
	var resp agent.StatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Overall != "degraded" {
		t.Errorf("want overall=degraded, got %q", resp.Overall)
	}
}

// TestStatusEndpointVersion verifies the version field is present.
func TestStatusEndpointVersion(t *testing.T) {
	socketPath, stop := startFakeAgent(t, fakeAgentMux())
	defer stop()

	store := newMetricsStore()
	h := &handlers{agentClient: agent.New(socketPath, 500*time.Millisecond), store: store}

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rr := httptest.NewRecorder()
	h.statusHandler(rr, req)

	var resp agent.StatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// version == "dev" in tests (ldflags not set)
	if resp.Version == "" {
		t.Error("version field must not be empty")
	}
}

// TestRateLimitMiddlewareIncrementsCounter verifies that a rate-limited request
// increments MetricsStore.rateLimited.
func TestRateLimitMiddlewareIncrementsCounter(t *testing.T) {
	m := newMetricsStore()
	rl := newRateLimiter(1.0, 1)
	rl.allow("192.0.2.5") // exhaust the single token
	h := rateLimitMiddleware(http.HandlerFunc(okHandler), rl, m)

	req := httptest.NewRequest(http.MethodPost, "/chat", nil)
	req.RemoteAddr = "192.0.2.5:1234"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", rr.Code)
	}
	if got := m.rateLimited.Load(); got != 1 {
		t.Errorf("rateLimited counter: want 1, got %d", got)
	}
}

// TestConcurrencyMiddlewareIncrementsCounter verifies that a concurrency-
// rejected request increments MetricsStore.concurrencyBusy.
func TestConcurrencyMiddlewareIncrementsCounter(t *testing.T) {
	m := newMetricsStore()
	sem := make(chan struct{}, 1)
	sem <- struct{}{} // fill the only slot
	h := concurrencyMiddleware(http.HandlerFunc(okHandler), sem, m)

	req := httptest.NewRequest(http.MethodPost, "/chat", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", rr.Code)
	}
	if got := m.concurrencyBusy.Load(); got != 1 {
		t.Errorf("concurrencyBusy counter: want 1, got %d", got)
	}
}
