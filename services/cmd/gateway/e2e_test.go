package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/agent"
)

// buildTestServer wires the full middleware stack (auth, rate-limit,
// concurrency, security headers) in front of real handlers backed by a fake
// agent, and returns an httptest.Server. Caller must close it.
func buildTestServer(t *testing.T, tok string) (*httptest.Server, func()) {
	t.Helper()
	socketPath, stopAgent := startFakeAgent(t, fakeAgentMux())

	store := newMetricsStore()
	h := newHandlers(socketPath, store)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /version", h.version)
	mux.HandleFunc("POST /chat", h.chat)
	mux.HandleFunc("GET /tools", h.tools)
	mux.HandleFunc("GET /metrics", h.metricsHandler)

	rl := newRateLimiter(defaultRPS, defaultBurst)
	sem := make(chan struct{}, maxConcurrent)

	var handler http.Handler = mux
	handler = concurrencyMiddleware(handler, sem, store)
	handler = rateLimitMiddleware(handler, rl, store)
	handler = bearerAuthMiddleware(handler, staticToken(tok))
	handler = securityHeadersMiddleware(handler)

	srv := httptest.NewServer(handler)
	return srv, func() {
		srv.Close()
		stopAgent()
	}
}

// TestE2EHealthzExemptFromAuth verifies /healthz is reachable without a token
// even when auth is configured.
func TestE2EHealthzExemptFromAuth(t *testing.T) {
	srv, stop := buildTestServer(t, "supersecret")
	defer stop()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}

// TestE2ESecurityHeaders verifies all security headers are present.
func TestE2ESecurityHeaders(t *testing.T) {
	srv, stop := buildTestServer(t, "")
	defer stop()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	resp.Body.Close()

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	}
	for hdr, val := range want {
		if got := resp.Header.Get(hdr); got != val {
			t.Errorf("%s: want %q, got %q", hdr, val, got)
		}
	}
}

// TestE2EAuthProtectsEndpoints verifies that protected endpoints return 401
// when no token is provided and 200 when the correct token is sent.
func TestE2EAuthProtectsEndpoints(t *testing.T) {
	srv, stop := buildTestServer(t, "tok123")
	defer stop()

	endpoints := []string{"/version", "/tools", "/metrics"}

	for _, ep := range endpoints {
		t.Run("unauth"+ep, func(t *testing.T) {
			resp, err := http.Get(srv.URL + ep)
			if err != nil {
				t.Fatalf("GET %s: %v", ep, err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("want 401 without token on %s, got %d", ep, resp.StatusCode)
			}
		})

		t.Run("auth"+ep, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, srv.URL+ep, nil)
			req.Header.Set("Authorization", "Bearer tok123")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("GET %s with token: %v", ep, err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("want 200 with valid token on %s, got %d", ep, resp.StatusCode)
			}
		})
	}
}

// TestE2EChatSSEStream verifies the full SSE streaming path from client to
// fake agent and back.
func TestE2EChatSSEStream(t *testing.T) {
	srv, stop := buildTestServer(t, "")
	defer stop()

	body := `{"messages":[{"role":"user","content":"hello"}]}`
	resp, err := http.Post(
		srv.URL+"/chat",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST /chat: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("want 200, got %d; body=%s", resp.StatusCode, data)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}

	// Read SSE lines and look for a token event.
	scanner := bufio.NewScanner(resp.Body)
	var sawData bool
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			sawData = true
			break
		}
	}
	if !sawData {
		t.Error("no 'data:' line received in SSE stream")
	}
}

// TestE2EChatRejectsMissingContentType verifies 415 for wrong content type.
func TestE2EChatRejectsMissingContentType(t *testing.T) {
	srv, stop := buildTestServer(t, "")
	defer stop()

	resp, err := http.Post(srv.URL+"/chat", "text/plain", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST /chat: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("want 415, got %d", resp.StatusCode)
	}
}

// TestE2EMetricsAggregation verifies that request counters increment after
// real requests through the middleware stack.
func TestE2EMetricsAggregation(t *testing.T) {
	socketPath, stopAgent := startFakeAgent(t, fakeAgentMux())
	defer stopAgent()

	store := newMetricsStore()
	h := newHandlers(socketPath, store)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /metrics", h.metricsHandler)

	var handler http.Handler = mux
	rl := newRateLimiter(defaultRPS, defaultBurst)
	sem := make(chan struct{}, maxConcurrent)
	handler = concurrencyMiddleware(handler, sem, store)
	handler = rateLimitMiddleware(handler, rl, store)
	handler = bearerAuthMiddleware(handler, staticToken(""))
	handler = securityHeadersMiddleware(handler)

	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Make two requests.
	for i := 0; i < 2; i++ {
		resp, err := http.Get(srv.URL + "/healthz")
		if err != nil {
			t.Fatalf("GET /healthz #%d: %v", i, err)
		}
		resp.Body.Close()
	}

	// Fetch metrics.
	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if !strings.Contains(string(body), "nura_gateway_requests_total") {
		t.Errorf("metrics missing nura_gateway_requests_total; got:\n%s", body)
	}
}

// TestE2EVersionJSON verifies /version returns parseable JSON with expected keys.
func TestE2EVersionJSON(t *testing.T) {
	srv, stop := buildTestServer(t, "")
	defer stop()

	resp, err := http.Get(srv.URL + "/version")
	if err != nil {
		t.Fatalf("GET /version: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var out map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("JSON decode /version: %v", err)
	}
	for _, key := range []string{"version", "service"} {
		if _, ok := out[key]; !ok {
			t.Errorf("/version JSON missing key %q; got: %v", key, out)
		}
	}
}

// TestE2EConcurrencyCapBlocks verifies the concurrency middleware returns 429
// when the semaphore is full, as seen from real HTTP responses.
func TestE2EConcurrencyCapBlocks(t *testing.T) {
	socketPath, stopAgent := startFakeAgent(t, fakeAgentMux())
	defer stopAgent()

	store := newMetricsStore()
	h := newHandlers(socketPath, store)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /version", h.version)

	sem := make(chan struct{}, 1)
	sem <- struct{}{} // fill the only slot

	var handler http.Handler = mux
	handler = concurrencyMiddleware(handler, sem, store)
	handler = rateLimitMiddleware(handler, newRateLimiter(defaultRPS, defaultBurst), store)
	handler = bearerAuthMiddleware(handler, staticToken(""))
	handler = securityHeadersMiddleware(handler)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	testSrv := &http.Server{Handler: handler}
	go func() { _ = testSrv.Serve(ln) }()
	defer testSrv.Close()

	// Use /version, which is NOT exempt from the concurrency cap.
	url := fmt.Sprintf("http://%s/version", ln.Addr().String())

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET /version: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("want 429 when at capacity, got %d", resp.StatusCode)
	}
}

// TestE2EToolsResponseShape verifies GET /tools returns valid JSON with a
// tools array.
func TestE2EToolsResponseShape(t *testing.T) {
	srv, stop := buildTestServer(t, "")
	defer stop()

	resp, err := http.Get(srv.URL + "/tools")
	if err != nil {
		t.Fatalf("GET /tools: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var out struct {
		Tools []agent.ToolInfo `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("JSON decode /tools: %v", err)
	}
	if len(out.Tools) == 0 {
		t.Error("GET /tools returned empty tools array")
	}
	if out.Tools[0].Name == "" {
		t.Error("first tool has empty name")
	}
}
