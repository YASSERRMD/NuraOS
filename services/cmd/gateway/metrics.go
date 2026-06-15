package main

import (
	"fmt"
	"io"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/agent"
)

// epIdx identifies a gateway endpoint for per-route request counters.
type epIdx int

const (
	epHealthz epIdx = iota
	epVersion
	epChat
	epTools
	epMetrics
	epStatus
	epCount
)

var epNames = [epCount]string{"healthz", "version", "chat", "tools", "metrics", "status"}

// MetricsStore accumulates gateway-level operational counters.
// All fields use atomic operations; the struct is safe for concurrent use
// without any external locking.
type MetricsStore struct {
	startTime time.Time

	reqTotal        [epCount]atomic.Int64
	rateLimited     atomic.Int64
	concurrencyBusy atomic.Int64

	chatLatUS    atomic.Int64 // cumulative /chat latency in microseconds
	chatLatCount atomic.Int64 // number of /chat responses completed
}

func newMetricsStore() *MetricsStore {
	return &MetricsStore{startTime: time.Now()}
}

// All increment methods are nil-safe so callers that pass a nil store
// (e.g., unit tests that only care about routing logic) never panic.

func (m *MetricsStore) incRequest(ep epIdx) {
	if m == nil {
		return
	}
	m.reqTotal[ep].Add(1)
}

func (m *MetricsStore) incRateLimited() {
	if m == nil {
		return
	}
	m.rateLimited.Add(1)
}

func (m *MetricsStore) incConcurrencyBusy() {
	if m == nil {
		return
	}
	m.concurrencyBusy.Add(1)
}

func (m *MetricsStore) recordChatLatency(d time.Duration) {
	if m == nil {
		return
	}
	m.chatLatUS.Add(int64(d / time.Microsecond))
	m.chatLatCount.Add(1)
}

// uptimeSeconds returns seconds elapsed since the store was created, or 0 for
// a nil receiver. Used by handlers that embed uptime in responses.
func (m *MetricsStore) uptimeSeconds() int64 {
	if m == nil {
		return 0
	}
	return int64(time.Since(m.startTime).Seconds())
}

// WriteTo writes Prometheus text exposition (version 0.0.4) to w.
// agentMet may be nil when the agent is unreachable; those metric families
// are omitted rather than emitted with stale zeros.
// A nil receiver emits only a comment line so callers need not nil-check.
func (m *MetricsStore) WriteTo(w io.Writer, agentMet *agent.AgentMetrics) {
	if m == nil {
		fmt.Fprintf(w, "# metrics unavailable\n")
		return
	}
	uptime := int64(time.Since(m.startTime).Seconds())

	fmt.Fprintf(w, "# HELP nura_gateway_uptime_seconds Seconds since the gateway process started.\n")
	fmt.Fprintf(w, "# TYPE nura_gateway_uptime_seconds gauge\n")
	fmt.Fprintf(w, "nura_gateway_uptime_seconds %d\n", uptime)

	fmt.Fprintf(w, "# HELP nura_gateway_requests_total HTTP requests served per endpoint.\n")
	fmt.Fprintf(w, "# TYPE nura_gateway_requests_total counter\n")
	for i := epIdx(0); i < epCount; i++ {
		fmt.Fprintf(w, "nura_gateway_requests_total{endpoint=%q} %d\n", epNames[i], m.reqTotal[i].Load())
	}

	fmt.Fprintf(w, "# HELP nura_gateway_rate_limited_total Requests rejected by the per-IP rate limiter.\n")
	fmt.Fprintf(w, "# TYPE nura_gateway_rate_limited_total counter\n")
	fmt.Fprintf(w, "nura_gateway_rate_limited_total %d\n", m.rateLimited.Load())

	fmt.Fprintf(w, "# HELP nura_gateway_concurrency_rejected_total Requests rejected by the global concurrency cap.\n")
	fmt.Fprintf(w, "# TYPE nura_gateway_concurrency_rejected_total counter\n")
	fmt.Fprintf(w, "nura_gateway_concurrency_rejected_total %d\n", m.concurrencyBusy.Load())

	fmt.Fprintf(w, "# HELP nura_gateway_chat_latency_microseconds_total Cumulative /chat handler latency in microseconds.\n")
	fmt.Fprintf(w, "# TYPE nura_gateway_chat_latency_microseconds_total counter\n")
	fmt.Fprintf(w, "nura_gateway_chat_latency_microseconds_total %d\n", m.chatLatUS.Load())

	fmt.Fprintf(w, "# HELP nura_gateway_chat_requests_completed_total /chat requests that received a complete response.\n")
	fmt.Fprintf(w, "# TYPE nura_gateway_chat_requests_completed_total counter\n")
	fmt.Fprintf(w, "nura_gateway_chat_requests_completed_total %d\n", m.chatLatCount.Load())

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Fprintf(w, "# HELP process_resident_memory_bytes Memory obtained from the OS by the Go runtime in bytes.\n")
	fmt.Fprintf(w, "# TYPE process_resident_memory_bytes gauge\n")
	fmt.Fprintf(w, "process_resident_memory_bytes %d\n", ms.Sys)

	if agentMet == nil {
		return
	}

	fmt.Fprintf(w, "# HELP nura_agent_uptime_seconds Seconds since the nura-agent process started.\n")
	fmt.Fprintf(w, "# TYPE nura_agent_uptime_seconds gauge\n")
	fmt.Fprintf(w, "nura_agent_uptime_seconds %d\n", agentMet.UptimeSeconds)

	fmt.Fprintf(w, "# HELP nura_agent_tokens_in_total Prompt tokens consumed across all turns.\n")
	fmt.Fprintf(w, "# TYPE nura_agent_tokens_in_total counter\n")
	fmt.Fprintf(w, "nura_agent_tokens_in_total %d\n", agentMet.TokensIn)

	fmt.Fprintf(w, "# HELP nura_agent_tokens_out_total Completion tokens generated across all turns.\n")
	fmt.Fprintf(w, "# TYPE nura_agent_tokens_out_total counter\n")
	fmt.Fprintf(w, "nura_agent_tokens_out_total %d\n", agentMet.TokensOut)

	fmt.Fprintf(w, "# HELP nura_agent_turns_total Completed inference turns.\n")
	fmt.Fprintf(w, "# TYPE nura_agent_turns_total counter\n")
	fmt.Fprintf(w, "nura_agent_turns_total %d\n", agentMet.TurnsTotal)

	if len(agentMet.ToolCallsTotal) > 0 {
		fmt.Fprintf(w, "# HELP nura_agent_tool_calls_total Tool invocations by tool name.\n")
		fmt.Fprintf(w, "# TYPE nura_agent_tool_calls_total counter\n")
		for name, count := range agentMet.ToolCallsTotal {
			fmt.Fprintf(w, "nura_agent_tool_calls_total{tool=%q} %d\n", name, count)
		}
	}

	if len(agentMet.ProviderRequests) > 0 {
		fmt.Fprintf(w, "# HELP nura_agent_provider_requests_total Inference requests sent to each provider.\n")
		fmt.Fprintf(w, "# TYPE nura_agent_provider_requests_total counter\n")
		for name, count := range agentMet.ProviderRequests {
			fmt.Fprintf(w, "nura_agent_provider_requests_total{provider=%q} %d\n", name, count)
		}
	}
}
