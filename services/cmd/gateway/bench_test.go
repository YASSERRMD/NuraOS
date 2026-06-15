package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/agent"
)

// startBenchAgent is startFakeAgent adapted for *testing.B.
func startBenchAgent(b *testing.B, handler http.Handler) (string, func()) {
	b.Helper()
	dir, err := os.MkdirTemp("/tmp", "nab")
	if err != nil {
		b.Fatalf("os.MkdirTemp: %v", err)
	}
	socketPath := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		os.RemoveAll(dir)
		b.Fatalf("listen unix %s: %v", socketPath, err)
	}
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(ln) }()
	return socketPath, func() { _ = srv.Close(); os.RemoveAll(dir) }
}

// BenchmarkChatHandler measures the SSE proxy path from the gateway perspective
// using a fake in-process agent. The agent sends a single token then done.
func BenchmarkChatHandler(b *testing.B) {
	socketPath, stop := startBenchAgent(b, fakeAgentMux())
	b.Cleanup(stop)

	h := &handlers{
		agentClient: agent.New(socketPath, 500*time.Millisecond),
		store:       newMetricsStore(),
	}
	body := `{"messages":[{"role":"user","content":"bench"}]}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		h.chat(rr, req)
		if rr.Code != http.StatusOK {
			b.Fatalf("want 200, got %d", rr.Code)
		}
	}
}

// BenchmarkRateLimiter measures throughput of the per-IP token-bucket allow()
// check, which is on the critical path for every non-healthz request.
func BenchmarkRateLimiter(b *testing.B) {
	rl := newRateLimiter(1e9, 1e9) // effectively unlimited for benchmarking
	ip := "127.0.0.1"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.allow(ip)
	}
}

// BenchmarkRateLimiterParallel measures concurrent throughput of the rate limiter.
func BenchmarkRateLimiterParallel(b *testing.B) {
	rl := newRateLimiter(1e9, 1e9)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rl.allow("10.0.0.1")
		}
	})
}

// BenchmarkMetricsWriteTo measures the cost of serialising the full Prometheus
// text output (gateway + agent metrics).
func BenchmarkMetricsWriteTo(b *testing.B) {
	store := newMetricsStore()
	store.incRequest(epChat)
	store.incRequest(epHealthz)
	store.recordChatLatency(42 * time.Millisecond)

	agentMet := &agent.AgentMetrics{
		TokensIn:   1234,
		TokensOut:  567,
		TurnsTotal: 10,
		ToolCallsTotal: map[string]int64{
			"system.info": 3,
		},
		ProviderRequests: map[string]int64{
			"local": 10,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		store.WriteTo(rr, agentMet, nil)
	}
}

// BenchmarkSecurityHeaders measures the middleware overhead per request.
func BenchmarkSecurityHeaders(b *testing.B) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := securityHeadersMiddleware(inner)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
	}
}

// BenchmarkConcurrencyMiddleware measures the semaphore acquire/release overhead.
func BenchmarkConcurrencyMiddleware(b *testing.B) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	sem := make(chan struct{}, maxConcurrent)
	h := concurrencyMiddleware(inner, sem, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/version", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
	}
}
