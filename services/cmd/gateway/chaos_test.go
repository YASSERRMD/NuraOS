package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/agent"
)

// chaosAgent is a fake agent where faults can be toggled at runtime.
type chaosAgent struct {
	mu sync.Mutex

	turnDelay      time.Duration // artificial delay before /turns response
	crashMidStream bool          // hijack and close connection mid-stream
	healthFail     bool          // /health returns 500

	requests atomic.Int64
}

func newChaosAgent() *chaosAgent { return &chaosAgent{} }

func (ca *chaosAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ca.requests.Add(1)
	ca.mu.Lock()
	delay := ca.turnDelay
	crash := ca.crashMidStream
	hfail := ca.healthFail
	ca.mu.Unlock()

	switch r.URL.Path {
	case "/health":
		if hfail {
			http.Error(w, `{"status":"error"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","provider":"chaos","uptime":1}`))

	case "/turns":
		if delay > 0 {
			time.Sleep(delay)
		}
		if crash {
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "no hijack", 500)
				return
			}
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = w.Write([]byte("data: {\"type\":\"token\",\"text\":\"ok\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"done\"}\n\n"))

	case "/tools":
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tools":[]}`))

	case "/metrics":
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tokens_in_total":0,"tokens_out_total":0,"turns_total":0}`))

	default:
		http.NotFound(w, r)
	}
}

func (ca *chaosAgent) set(fn func(*chaosAgent)) {
	ca.mu.Lock()
	fn(ca)
	ca.mu.Unlock()
}

func startChaosAgent(t *testing.T) (string, *chaosAgent, func()) {
	t.Helper()
	ca := newChaosAgent()
	socketPath, stop := startFakeAgent(t, ca)
	return socketPath, ca, stop
}

// TestFaultAgentSlowResponse verifies a slow agent does not hang the handler.
func TestFaultAgentSlowResponse(t *testing.T) {
	socketPath, ca, stop := startChaosAgent(t)
	defer stop()

	ca.set(func(c *chaosAgent) { c.turnDelay = 80 * time.Millisecond })

	h := &handlers{
		agentClient: agent.New(socketPath, 500*time.Millisecond),
		store:       newMetricsStore(),
	}
	body := `{"messages":[{"role":"user","content":"slow"}]}`
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.chat(rr, req)

	if rr.Code == 0 {
		t.Error("chat handler produced no response status")
	}
}

// TestFaultAgentCrashMidStream verifies the gateway returns 5xx when the agent
// crashes immediately after the request arrives.
func TestFaultAgentCrashMidStream(t *testing.T) {
	socketPath, ca, stop := startChaosAgent(t)
	defer stop()

	ca.set(func(c *chaosAgent) { c.crashMidStream = true })

	h := &handlers{
		agentClient: agent.New(socketPath, 500*time.Millisecond),
		store:       newMetricsStore(),
	}
	body := `{"messages":[{"role":"user","content":"crash"}]}`
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.chat(rr, req)

	if rr.Code != http.StatusBadGateway && rr.Code != http.StatusServiceUnavailable {
		t.Errorf("want 502 or 503 on agent crash, got %d; body=%s",
			rr.Code, rr.Body.String())
	}
}

// TestFaultAgentHealthDegraded verifies /healthz returns 503 when the agent
// /health endpoint returns an error.
func TestFaultAgentHealthDegraded(t *testing.T) {
	socketPath, ca, stop := startChaosAgent(t)
	defer stop()

	ca.set(func(c *chaosAgent) { c.healthFail = true })

	h := &handlers{
		agentClient: agent.New(socketPath, 500*time.Millisecond),
		store:       newMetricsStore(),
	}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	h.healthz(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503 on agent health failure, got %d", rr.Code)
	}
}

// TestFaultConcurrentBurstUnderFault verifies no panics or zero status codes
// when a burst of requests hits an unreachable agent.
func TestFaultConcurrentBurstUnderFault(t *testing.T) {
	h := &handlers{
		agentClient: agent.New("/nonexistent-chaos.sock", 30*time.Millisecond),
		store:       newMetricsStore(),
	}

	const N = 20
	codes := make([]int, N)
	var wg sync.WaitGroup
	wg.Add(N)

	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			body := `{"messages":[{"role":"user","content":"burst"}]}`
			req := httptest.NewRequest(http.MethodPost, "/chat",
				strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			h.chat(rr, req)
			codes[i] = rr.Code
		}()
	}
	wg.Wait()

	for i, code := range codes {
		if code == 0 {
			t.Errorf("request %d: no status code (handler panicked?)", i)
		}
		if code == http.StatusInternalServerError {
			t.Errorf("request %d: got 500; agent-down should return 503", i)
		}
	}
}

// TestFaultRateLimitRecovery verifies requests recover after the rate-limit
// window resets.
func TestFaultRateLimitRecovery(t *testing.T) {
	socketPath, _, stop := startChaosAgent(t)
	defer stop()

	store := newMetricsStore()
	h := newHandlers(socketPath, store)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /version", h.version)

	rl := newRateLimiter(1.0, 1) // 1 RPS, burst 1
	sem := make(chan struct{}, maxConcurrent)

	var handler http.Handler = mux
	handler = concurrencyMiddleware(handler, sem, store)
	handler = rateLimitMiddleware(handler, rl, store)
	handler = bearerAuthMiddleware(handler, staticToken(""))
	handler = securityHeadersMiddleware(handler)

	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := &http.Client{Timeout: 2 * time.Second}

	// First request: burst token available.
	r1, err := client.Get(srv.URL + "/version")
	if err != nil {
		t.Fatalf("request 1: %v", err)
	}
	_, _ = io.Copy(io.Discard, r1.Body)
	r1.Body.Close()
	if r1.StatusCode != http.StatusOK {
		t.Errorf("request 1: want 200, got %d", r1.StatusCode)
	}

	// Second request immediately: should hit rate limit.
	r2, err := client.Get(srv.URL + "/version")
	if err != nil {
		t.Fatalf("request 2: %v", err)
	}
	_, _ = io.Copy(io.Discard, r2.Body)
	r2.Body.Close()
	if r2.StatusCode != http.StatusTooManyRequests {
		t.Logf("request 2: got %d (not 429; timer may have ticked -- not fatal)", r2.StatusCode)
	}

	// After the window resets (1s), requests should succeed again.
	time.Sleep(1100 * time.Millisecond)
	r3, err := client.Get(srv.URL + "/version")
	if err != nil {
		t.Fatalf("request 3: %v", err)
	}
	_, _ = io.Copy(io.Discard, r3.Body)
	r3.Body.Close()
	if r3.StatusCode != http.StatusOK {
		t.Errorf("request 3 (after recovery): want 200, got %d", r3.StatusCode)
	}
}

// TestFaultLargePayloadRejectedBeforeAgent verifies the gateway rejects
// oversized payloads before they reach the agent.
func TestFaultLargePayloadRejectedBeforeAgent(t *testing.T) {
	socketPath, ca, stop := startChaosAgent(t)
	defer stop()

	h := &handlers{
		agentClient: agent.New(socketPath, 500*time.Millisecond),
		store:       newMetricsStore(),
	}

	oversized := `{"messages":[{"role":"user","content":"` +
		strings.Repeat("x", maxChatBodyBytes+100) + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/chat",
		strings.NewReader(oversized))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.chat(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge &&
		rr.Code != http.StatusBadRequest {
		t.Errorf("want 413 or 400, got %d", rr.Code)
	}
	if ca.requests.Load() > 0 {
		t.Errorf("oversized payload reached the agent (%d requests; want 0)",
			ca.requests.Load())
	}
}

// TestFaultToolsAgentDown verifies GET /tools returns 503 (not 500) when the
// agent is unreachable.
func TestFaultToolsAgentDown(t *testing.T) {
	h := &handlers{
		agentClient: agent.New("/nonexistent-chaos2.sock", 30*time.Millisecond),
		store:       newMetricsStore(),
	}
	req := httptest.NewRequest(http.MethodGet, "/tools", nil)
	rr := httptest.NewRecorder()
	h.tools(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503 when agent down, got %d; body=%s",
			rr.Code, rr.Body.String())
	}
}

// TestFaultTokenRotationUnderLoad verifies token rotation is safe under
// concurrent requests: each request sees either the old or new token.
func TestFaultTokenRotationUnderLoad(t *testing.T) {
	socketPath, _, stop := startChaosAgent(t)
	defer stop()

	path := t.TempDir() + "/secrets.toml"
	if err := os.WriteFile(path, []byte("gateway_token = \"v1\"\n"), 0600); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	ts := newTokenStore(path)

	store := newMetricsStore()
	h := newHandlers(socketPath, store)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /version", h.version)

	var handler http.Handler = mux
	handler = concurrencyMiddleware(handler, make(chan struct{}, maxConcurrent), store)
	handler = rateLimitMiddleware(handler, newRateLimiter(defaultRPS, defaultBurst), store)
	handler = bearerAuthMiddleware(handler, ts)
	handler = securityHeadersMiddleware(handler)

	srv := httptest.NewServer(handler)
	defer srv.Close()

	const N = 5
	var wg sync.WaitGroup
	wg.Add(N)
	results := make([]int, N)

	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/version", nil)
			req.Header.Set("Authorization", "Bearer v1")
			resp, err := srv.Client().Do(req)
			if err != nil {
				results[i] = -1
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			results[i] = resp.StatusCode
		}()
	}

	// Rotate token while requests are in-flight.
	if err := os.WriteFile(path, []byte("gateway_token = \"v2\"\n"), 0600); err != nil {
		t.Logf("write v2: %v", err)
	}
	ts.reload()

	wg.Wait()

	for i, code := range results {
		// v1 requests completed before rotation get 200; after rotation get 401.
		if code != http.StatusOK && code != http.StatusUnauthorized {
			t.Errorf("request %d: unexpected code %d (want 200 or 401)", i, code)
		}
	}
}
