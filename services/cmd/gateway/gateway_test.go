package main

import (
	"context"
	"encoding/json"
	"fmt"
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

// startFakeAgent starts an HTTP server on a Unix socket under /tmp (not
// t.TempDir, whose path can exceed the 104-char macOS socket-path limit)
// and returns the socket path and a stop function.
func startFakeAgent(t *testing.T, handler http.Handler) (string, func()) {
	t.Helper()
	// MkdirTemp under /tmp keeps paths short (e.g. /tmp/na123456/s).
	dir, err := os.MkdirTemp("/tmp", "na")
	if err != nil {
		t.Fatalf("os.MkdirTemp: %v", err)
	}
	socketPath := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("listen unix %s: %v", socketPath, err)
	}
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(ln) }()
	return socketPath, func() { _ = srv.Close(); os.RemoveAll(dir) }
}

func fakeAgentMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(agent.HealthResponse{
			Status:   "ok",
			Provider: "test",
			Uptime:   1,
		})
	})

	mux.HandleFunc("/turns", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"type\":\"token\",\"text\":\"hello\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"done\"}\n\n")
	})

	mux.HandleFunc("/tools", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(agent.ToolsResponse{
			Tools: []agent.ToolInfo{
				{Name: "system.info", Description: "System information", ReadOnly: true},
			},
		})
	})

	return mux
}

func TestHealthzOK(t *testing.T) {
	socketPath, stop := startFakeAgent(t, fakeAgentMux())
	defer stop()

	h := &handlers{agentClient: agent.New(socketPath, 500*time.Millisecond)}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	h.healthz(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"agent_reachable": true`) {
		t.Errorf("body %q: missing agent_reachable:true", rr.Body.String())
	}
}

func TestHealthzDegraded(t *testing.T) {
	h := &handlers{agentClient: agent.New("/nonexistent-p29.sock", 100*time.Millisecond)}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	h.healthz(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "degraded") {
		t.Errorf("body %q: missing 'degraded'", rr.Body.String())
	}
}

func TestChatStreamBasic(t *testing.T) {
	socketPath, stop := startFakeAgent(t, fakeAgentMux())
	defer stop()

	h := &handlers{agentClient: agent.New(socketPath, 500*time.Millisecond)}
	body := `{"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.chat(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type: want SSE, got %q", ct)
	}
	if !strings.Contains(rr.Body.String(), "hello") {
		t.Errorf("body %q: missing expected token", rr.Body.String())
	}
}

func TestChatWrongContentType(t *testing.T) {
	h := &handlers{agentClient: agent.New("/nonexistent-p29.sock", 100*time.Millisecond)}
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "text/plain")
	rr := httptest.NewRecorder()
	h.chat(rr, req)

	if rr.Code != http.StatusUnsupportedMediaType {
		t.Errorf("want 415, got %d", rr.Code)
	}
}

func TestChatBodyTooLarge(t *testing.T) {
	h := &handlers{agentClient: agent.New("/nonexistent-p29.sock", 100*time.Millisecond)}
	bigContent := `{"messages":[{"role":"user","content":"` +
		strings.Repeat("x", maxChatBodyBytes) + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(bigContent))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.chat(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge && rr.Code != http.StatusBadRequest {
		t.Errorf("want 413 or 400, got %d", rr.Code)
	}
}

func TestChatEmptyMessages(t *testing.T) {
	h := &handlers{agentClient: agent.New("/nonexistent-p29.sock", 100*time.Millisecond)}
	req := httptest.NewRequest(http.MethodPost, "/chat",
		strings.NewReader(`{"messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.chat(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rr.Code)
	}
}

func TestChatAgentUnavailable(t *testing.T) {
	h := &handlers{agentClient: agent.New("/nonexistent-p29.sock", 50*time.Millisecond)}
	body := `{"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.chat(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", rr.Code)
	}
}

func TestChatClientDisconnectExitsHandler(t *testing.T) {
	// The fake agent sends response headers and then keeps the connection open.
	// We verify the gateway's chat handler exits promptly after the request
	// context is cancelled (simulating a client disconnect).
	started := make(chan struct{})

	slowAgent := http.NewServeMux()
	slowAgent.HandleFunc("/turns", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		close(started)
		<-r.Context().Done() // wait until connection is dropped
	})

	socketPath, stop := startFakeAgent(t, slowAgent)
	defer stop()

	h := &handlers{agentClient: agent.New(socketPath, 500*time.Millisecond)}

	ctx, cancel := context.WithCancel(context.Background())
	body := `{"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.chat(rr, req)
		close(done)
	}()

	<-started // fake agent sent headers; gateway is in the SSE proxy loop
	cancel()  // simulate client disconnect

	select {
	case <-done:
		// handler exited promptly: pass
	case <-time.After(2 * time.Second):
		t.Error("chat handler did not exit within 2 s after client disconnect")
	}
}

func TestToolsEndpoint(t *testing.T) {
	socketPath, stop := startFakeAgent(t, fakeAgentMux())
	defer stop()

	h := &handlers{agentClient: agent.New(socketPath, 500*time.Millisecond)}
	req := httptest.NewRequest(http.MethodGet, "/tools", nil)
	rr := httptest.NewRecorder()
	h.tools(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "system.info") {
		t.Errorf("body %q: missing expected tool name", rr.Body.String())
	}
}

func TestToolsAgentUnavailable(t *testing.T) {
	h := &handlers{agentClient: agent.New("/nonexistent-p29.sock", 50*time.Millisecond)}
	req := httptest.NewRequest(http.MethodGet, "/tools", nil)
	rr := httptest.NewRecorder()
	h.tools(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", rr.Code)
	}
}
