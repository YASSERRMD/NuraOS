package main

import (
	"fmt"
	"io"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/agent"
	"github.com/yasserrmd/nuraos/services/internal/cgroup"
	"github.com/yasserrmd/nuraos/services/internal/diskmon"
	"github.com/yasserrmd/nuraos/services/internal/providerhealth"
	"github.com/yasserrmd/nuraos/services/internal/sysmetrics"
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
	epConfig
	epModels
	epUpdateStatus
	epTelemetryStatus
	epBoard
	epCount
)

var epNames = [epCount]string{"healthz", "version", "chat", "tools", "metrics", "status", "config", "models", "update_status", "telemetry_status", "board"}

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
// agentMet may be nil when the agent is unreachable; those metric families are
// omitted rather than emitted with stale zeros. disk may be nil when the
// monitor has not yet polled or is unconfigured. cgStats may be nil when
// cgroup metrics are unavailable. provSnap may be nil or empty when provider
// health monitoring is not configured. A nil receiver emits only a comment line.
func (m *MetricsStore) WriteTo(w io.Writer, agentMet *agent.AgentMetrics, disk *diskmon.Usage, cgStats map[string]*cgroup.Stats, provSnap []providerhealth.Status) {
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

	if disk != nil {
		fmt.Fprintf(w, "# HELP nura_disk_total_bytes Total size of the /data filesystem in bytes.\n")
		fmt.Fprintf(w, "# TYPE nura_disk_total_bytes gauge\n")
		fmt.Fprintf(w, "nura_disk_total_bytes %d\n", disk.Total)

		fmt.Fprintf(w, "# HELP nura_disk_used_bytes Used bytes on the /data filesystem.\n")
		fmt.Fprintf(w, "# TYPE nura_disk_used_bytes gauge\n")
		fmt.Fprintf(w, "nura_disk_used_bytes %d\n", disk.Used)

		fmt.Fprintf(w, "# HELP nura_disk_available_bytes Available bytes on the /data filesystem.\n")
		fmt.Fprintf(w, "# TYPE nura_disk_available_bytes gauge\n")
		fmt.Fprintf(w, "nura_disk_available_bytes %d\n", disk.Available)

		fmt.Fprintf(w, "# HELP nura_disk_used_percent Percentage of /data filesystem in use (0-100).\n")
		fmt.Fprintf(w, "# TYPE nura_disk_used_percent gauge\n")
		fmt.Fprintf(w, "nura_disk_used_percent %.2f\n", disk.UsedPct)
	}

	if len(cgStats) > 0 {
		fmt.Fprintf(w, "# HELP nura_cgroup_cpu_usage_seconds_total Total CPU time consumed by a service cgroup.\n")
		fmt.Fprintf(w, "# TYPE nura_cgroup_cpu_usage_seconds_total counter\n")
		for svc, s := range cgStats {
			if s != nil {
				fmt.Fprintf(w, "nura_cgroup_cpu_usage_seconds_total{service=%q} %.6f\n", svc, float64(s.CPUUsageUsec)/1e6)
			}
		}

		fmt.Fprintf(w, "# HELP nura_cgroup_memory_bytes Current memory usage of a service cgroup in bytes.\n")
		fmt.Fprintf(w, "# TYPE nura_cgroup_memory_bytes gauge\n")
		for svc, s := range cgStats {
			if s != nil {
				fmt.Fprintf(w, "nura_cgroup_memory_bytes{service=%q} %d\n", svc, s.MemoryCurrent)
			}
		}

		fmt.Fprintf(w, "# HELP nura_cgroup_memory_max_bytes Configured memory hard limit of a service cgroup (0 = unlimited).\n")
		fmt.Fprintf(w, "# TYPE nura_cgroup_memory_max_bytes gauge\n")
		for svc, s := range cgStats {
			if s != nil {
				fmt.Fprintf(w, "nura_cgroup_memory_max_bytes{service=%q} %d\n", svc, s.MemoryMax)
			}
		}

		fmt.Fprintf(w, "# HELP nura_cgroup_oom_kills_total Total OOM kills in a service cgroup.\n")
		fmt.Fprintf(w, "# TYPE nura_cgroup_oom_kills_total counter\n")
		for svc, s := range cgStats {
			if s != nil {
				fmt.Fprintf(w, "nura_cgroup_oom_kills_total{service=%q} %d\n", svc, s.OOMKills)
			}
		}
	}

	if len(provSnap) > 0 {
		// circuit_state encoded as: 0=closed, 1=half-open, 2=open
		circuitStateVal := func(s string) int {
			switch s {
			case "open":
				return 2
			case "half-open":
				return 1
			default:
				return 0
			}
		}
		fmt.Fprintf(w, "# HELP nura_provider_circuit_breaker_state Provider circuit breaker state: 0=closed, 1=half-open, 2=open.\n")
		fmt.Fprintf(w, "# TYPE nura_provider_circuit_breaker_state gauge\n")
		for _, ps := range provSnap {
			fmt.Fprintf(w, "nura_provider_circuit_breaker_state{provider=%q} %d\n", ps.Name, circuitStateVal(ps.CircuitState))
		}

		fmt.Fprintf(w, "# HELP nura_provider_probe_success_total Cumulative successful health probes per provider.\n")
		fmt.Fprintf(w, "# TYPE nura_provider_probe_success_total counter\n")
		for _, ps := range provSnap {
			fmt.Fprintf(w, "nura_provider_probe_success_total{provider=%q} %d\n", ps.Name, ps.ProbeSuccesses)
		}

		fmt.Fprintf(w, "# HELP nura_provider_probe_failure_total Cumulative failed health probes per provider.\n")
		fmt.Fprintf(w, "# TYPE nura_provider_probe_failure_total counter\n")
		for _, ps := range provSnap {
			fmt.Fprintf(w, "nura_provider_probe_failure_total{provider=%q} %d\n", ps.Name, ps.ProbeFailures)
		}
	}

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

// WriteSysMetrics appends OS-level system metrics to w in Prometheus text format.
// A nil receiver is a no-op. sys is the snapshot from sysmetrics.Collect; a
// zero-value Stats silently emits only the non-zero fields.
func (m *MetricsStore) WriteSysMetrics(w io.Writer, sys sysmetrics.Stats) {
	if m == nil {
		return
	}

	if sys.EntropyAvailBits > 0 {
		fmt.Fprintf(w, "# HELP nura_entropy_avail_bits Kernel CSPRNG available entropy bits (/proc/sys/kernel/random/entropy_avail).\n")
		fmt.Fprintf(w, "# TYPE nura_entropy_avail_bits gauge\n")
		fmt.Fprintf(w, "nura_entropy_avail_bits %d\n", sys.EntropyAvailBits)
	}

	if len(sys.Interfaces) > 0 {
		fmt.Fprintf(w, "# HELP nura_net_rx_bytes_total Total bytes received per network interface.\n")
		fmt.Fprintf(w, "# TYPE nura_net_rx_bytes_total counter\n")
		for _, iface := range sys.Interfaces {
			fmt.Fprintf(w, "nura_net_rx_bytes_total{interface=%q} %d\n", iface.Name, iface.RxBytes)
		}

		fmt.Fprintf(w, "# HELP nura_net_tx_bytes_total Total bytes transmitted per network interface.\n")
		fmt.Fprintf(w, "# TYPE nura_net_tx_bytes_total counter\n")
		for _, iface := range sys.Interfaces {
			fmt.Fprintf(w, "nura_net_tx_bytes_total{interface=%q} %d\n", iface.Name, iface.TxBytes)
		}

		fmt.Fprintf(w, "# HELP nura_net_rx_packets_total Total packets received per network interface.\n")
		fmt.Fprintf(w, "# TYPE nura_net_rx_packets_total counter\n")
		for _, iface := range sys.Interfaces {
			fmt.Fprintf(w, "nura_net_rx_packets_total{interface=%q} %d\n", iface.Name, iface.RxPkts)
		}

		fmt.Fprintf(w, "# HELP nura_net_tx_packets_total Total packets transmitted per network interface.\n")
		fmt.Fprintf(w, "# TYPE nura_net_tx_packets_total counter\n")
		for _, iface := range sys.Interfaces {
			fmt.Fprintf(w, "nura_net_tx_packets_total{interface=%q} %d\n", iface.Name, iface.TxPkts)
		}

		fmt.Fprintf(w, "# HELP nura_net_rx_drop_total Received packets dropped per network interface.\n")
		fmt.Fprintf(w, "# TYPE nura_net_rx_drop_total counter\n")
		for _, iface := range sys.Interfaces {
			fmt.Fprintf(w, "nura_net_rx_drop_total{interface=%q} %d\n", iface.Name, iface.RxDrop)
		}

		fmt.Fprintf(w, "# HELP nura_net_tx_drop_total Transmitted packets dropped per network interface.\n")
		fmt.Fprintf(w, "# TYPE nura_net_tx_drop_total counter\n")
		for _, iface := range sys.Interfaces {
			fmt.Fprintf(w, "nura_net_tx_drop_total{interface=%q} %d\n", iface.Name, iface.TxDrop)
		}
	}
}
