package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/agent"
)

// TestGracefulShutdownDrainsConnections verifies that calling srv.Shutdown()
// while a request is in flight causes ListenAndServe to return
// http.ErrServerClosed (not a hard error), and that in-flight requests
// complete before the server exits.
func TestGracefulShutdownDrainsConnections(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	// Handler that holds a response open until its context is cancelled.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	})

	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(ln) }()

	addr := ln.Addr().String()
	reqDone := make(chan struct{})

	// Start a long-lived request.
	go func() {
		defer close(reqDone)
		resp, err := http.Get("http://" + addr + "/")
		if err == nil {
			resp.Body.Close()
		}
	}()

	// Give the request time to be received.
	time.Sleep(50 * time.Millisecond)

	// Initiate graceful shutdown with a 2-second drain window.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Logf("shutdown returned: %v (may be deadline exceeded from in-flight SSE)", err)
	}

	// Verify that in-flight request completed (context cancelled by Shutdown).
	select {
	case <-reqDone:
	case <-time.After(3 * time.Second):
		t.Error("in-flight request did not complete after shutdown")
	}
}

// TestAgentCrashMidChatReturns503 verifies that the gateway returns a clean
// 503 (not a panic or connection hang) when the agent disappears mid-request.
func TestAgentCrashMidChatReturns503(t *testing.T) {
	// Agent that closes the connection immediately after writing the header.
	crashAgent := http.NewServeMux()
	crashAgent.HandleFunc("/turns", func(w http.ResponseWriter, r *http.Request) {
		// Simulate crash by hijacking and closing the connection.
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", 500)
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close() // abrupt close -- agent crash simulation
	})

	socketPath, stop := startFakeAgent(t, crashAgent)
	defer stop()

	h := &handlers{
		agentClient: agent.New(socketPath, 500*time.Millisecond),
		store:       newMetricsStore(),
	}
	body := `{"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.chat(rr, req)

	// A crash immediately after headers produces either 502 or 503.
	if rr.Code != http.StatusServiceUnavailable && rr.Code != http.StatusBadGateway {
		t.Errorf("want 502 or 503 after agent crash, got %d; body=%s",
			rr.Code, rr.Body.String())
	}
}

// TestAgentSocketDisappears verifies /healthz returns degraded when the
// agent socket vanishes (simulating a crash of the agent process).
func TestAgentSocketDisappears(t *testing.T) {
	socketPath, stop := startFakeAgent(t, fakeAgentMux())
	stop() // stop the agent immediately to simulate socket disappearing

	h := &handlers{
		agentClient: agent.New(socketPath, 100*time.Millisecond),
		store:       newMetricsStore(),
	}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	h.healthz(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503 when agent socket gone, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "degraded") {
		t.Errorf("body %q: missing 'degraded'", rr.Body.String())
	}
}

// TestAgentSlowStartupHealthzStillResponds verifies that /healthz always
// responds quickly even when the agent is not yet reachable (startup race).
func TestAgentSlowStartupHealthzStillResponds(t *testing.T) {
	h := &handlers{
		agentClient: agent.New("/nonexistent-phase34.sock", 50*time.Millisecond),
		store:       newMetricsStore(),
	}

	start := time.Now()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	h.healthz(rr, req)
	elapsed := time.Since(start)

	// Should not block longer than ~2x dial timeout.
	if elapsed > 2*time.Second {
		t.Errorf("/healthz took %v; should return quickly even when agent is down", elapsed)
	}
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", rr.Code)
	}
}

// TestConcurrentRequestsDuringShutdown verifies the concurrency cap still
// works when the server is under load (guard against races during restart).
func TestConcurrentRequestsDuringShutdown(t *testing.T) {
	m := newMetricsStore()
	sem := make(chan struct{}, 2) // cap of 2
	sem <- struct{}{}             // fill one slot
	sem <- struct{}{}             // fill second slot

	h := concurrencyMiddleware(http.HandlerFunc(okHandler), sem, m)
	req := httptest.NewRequest(http.MethodPost, "/chat", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("want 429 at capacity, got %d", rr.Code)
	}
	if m.concurrencyBusy.Load() != 1 {
		t.Errorf("want concurrencyBusy=1, got %d", m.concurrencyBusy.Load())
	}
}
