package main

// Smoke matrix: exercise every registered endpoint through the full
// middleware stack (security headers, auth-bypass, rate limit, concurrency)
// using a fake agent on a Unix socket. Each row asserts minimum response shape.
// This test does NOT require a real agent, real model, or running OS.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func buildSmokeStack(t *testing.T) (http.Handler, func()) {
	t.Helper()
	socketPath, stopAgent := startFakeAgent(t, fakeAgentMux())

	tmp := t.TempDir()
	t.Setenv("MODEL_MANIFEST", filepath.Join(tmp, "model.json"))
	t.Setenv("MODEL_DIR", filepath.Join(tmp, "models"))
	t.Setenv("ACTIVE_SLOT_FILE", filepath.Join(tmp, "active-slot"))
	t.Setenv("UPDATE_STATE", filepath.Join(tmp, "update-state.json"))
	t.Setenv("NURA_TELEMETRY_FILE", filepath.Join(tmp, "telemetry.json"))
	t.Setenv("BOARD_INFO_FILE", filepath.Join(tmp, "board.json"))
	t.Setenv("NURA_TELEMETRY", "0")

	store := newMetricsStore()
	h := newHandlers(socketPath, store)
	ts := &tokenStore{path: filepath.Join(tmp, "secrets.toml")}
	h.ts = ts

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /version", h.version)
	mux.HandleFunc("POST /chat", h.chat)
	mux.HandleFunc("GET /tools", h.tools)
	mux.HandleFunc("GET /metrics", h.metricsHandler)
	mux.HandleFunc("GET /status", h.statusHandler)
	mux.HandleFunc("GET /config", h.configHandler)
	mux.HandleFunc("GET /models", h.modelsHandler)
	mux.HandleFunc("GET /update/status", h.updateStatusHandler)
	mux.HandleFunc("GET /telemetry/status", h.telemetryStatusHandler)
	mux.HandleFunc("GET /board", h.boardHandler)

	rl := newRateLimiter(defaultRPS, defaultBurst)
	sem := make(chan struct{}, maxConcurrent)
	var handler http.Handler = mux
	handler = concurrencyMiddleware(handler, sem, store)
	handler = rateLimitMiddleware(handler, rl, store)
	handler = bearerAuthMiddleware(handler, ts)
	handler = securityHeadersMiddleware(handler)

	return handler, stopAgent
}

func smokeGET(t *testing.T, handler http.Handler, path string) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var body map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&body)
	return w.Code, body
}

type smokeCase struct {
	method      string
	path        string
	wantCode    int
	wantKeys    []string // top-level JSON keys that must be present
	wantHeaders map[string]string
}

var smokeCases = []smokeCase{
	{
		method:   "GET",
		path:     "/healthz",
		wantCode: http.StatusOK,
		wantKeys: []string{"status"},
	},
	{
		method:   "GET",
		path:     "/version",
		wantCode: http.StatusOK,
		wantKeys: []string{"service", "version"},
	},
	{
		method:   "GET",
		path:     "/config",
		wantCode: http.StatusOK,
		wantKeys: []string{"gateway", "agent"},
	},
	{
		method:   "GET",
		path:     "/tools",
		wantCode: http.StatusOK,
		wantKeys: []string{"tools"},
	},
	{
		method:   "GET",
		path:     "/metrics",
		wantCode: http.StatusOK,
		wantKeys: nil, // Prometheus text format, not JSON
	},
	{
		method:   "GET",
		path:     "/status",
		wantCode: http.StatusOK,
		wantKeys: []string{"overall", "version", "components"},
	},
	{
		method:   "GET",
		path:     "/models",
		wantCode: http.StatusOK,
		wantKeys: []string{"active", "available"},
	},
	{
		method:   "GET",
		path:     "/update/status",
		wantCode: http.StatusOK,
		wantKeys: []string{"active_slot", "inactive_slot"},
	},
	{
		method:   "GET",
		path:     "/telemetry/status",
		wantCode: http.StatusOK,
		wantKeys: []string{"telemetry"},
	},
	{
		method:   "GET",
		path:     "/board",
		wantCode: http.StatusOK,
		wantKeys: []string{"board"},
	},
}

func TestSmokeMatrix(t *testing.T) {
	handler, stopAgent := buildSmokeStack(t)
	defer stopAgent()

	for _, tc := range smokeCases {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Errorf("path %s: expected status %d, got %d (body: %s)",
					tc.path, tc.wantCode, w.Code, w.Body.String())
				return
			}

			if len(tc.wantKeys) == 0 {
				return
			}

			var body map[string]interface{}
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("path %s: response not JSON: %v", tc.path, err)
			}
			for _, k := range tc.wantKeys {
				if _, ok := body[k]; !ok {
					t.Errorf("path %s: missing key %q in response", tc.path, k)
				}
			}
		})
	}
}

func TestSmokeSecurityHeaders(t *testing.T) {
	handler, stopAgent := buildSmokeStack(t)
	defer stopAgent()

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	required := []string{
		"X-Content-Type-Options",
		"X-Frame-Options",
		"Referrer-Policy",
	}
	for _, h := range required {
		if w.Header().Get(h) == "" {
			t.Errorf("security header %q missing from response", h)
		}
	}
}

func TestSmokeAllEndpointsIncrementCounters(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MODEL_MANIFEST", filepath.Join(tmp, "model.json"))
	t.Setenv("MODEL_DIR", filepath.Join(tmp, "models"))
	t.Setenv("ACTIVE_SLOT_FILE", filepath.Join(tmp, "active-slot"))
	t.Setenv("UPDATE_STATE", filepath.Join(tmp, "update-state.json"))
	t.Setenv("NURA_TELEMETRY_FILE", filepath.Join(tmp, "telemetry.json"))
	t.Setenv("BOARD_INFO_FILE", filepath.Join(tmp, "board.json"))
	t.Setenv("NURA_TELEMETRY", "0")

	socketPath, stopAgent := startFakeAgent(t, fakeAgentMux())
	defer stopAgent()

	store := newMetricsStore()
	h := newHandlers(socketPath, store)
	h.ts = &tokenStore{path: filepath.Join(tmp, "secrets.toml")}

	pairs := []struct {
		ep      epIdx
		handler func(http.ResponseWriter, *http.Request)
		path    string
	}{
		{epHealthz, h.healthz, "/healthz"},
		{epVersion, h.version, "/version"},
		{epTools, h.tools, "/tools"},
		{epMetrics, h.metricsHandler, "/metrics"},
		{epStatus, h.statusHandler, "/status"},
		{epConfig, h.configHandler, "/config"},
		{epModels, h.modelsHandler, "/models"},
		{epUpdateStatus, h.updateStatusHandler, "/update/status"},
		{epTelemetryStatus, h.telemetryStatusHandler, "/telemetry/status"},
		{epBoard, h.boardHandler, "/board"},
	}

	for _, p := range pairs {
		req := httptest.NewRequest(http.MethodGet, p.path, nil)
		w := httptest.NewRecorder()
		p.handler(w, req)
	}

	for _, p := range pairs {
		if got := store.reqTotal[p.ep].Load(); got != 1 {
			t.Errorf("endpoint %s counter: expected 1, got %d", p.path, got)
		}
	}
}

func TestSmokeChatBodyRequired(t *testing.T) {
	handler, stopAgent := buildSmokeStack(t)
	defer stopAgent()

	req := httptest.NewRequest(http.MethodPost, "/chat",
		strings.NewReader(`{"messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty messages, got %d", w.Code)
	}
}

func TestSmokeWriteBoardAndRead(t *testing.T) {
	tmp := t.TempDir()
	boardFile := filepath.Join(tmp, "board.json")
	if err := os.WriteFile(boardFile, []byte(`{"id":"qemu-x86_64","arch":"x86_64"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOARD_INFO_FILE", boardFile)
	t.Setenv("MODEL_MANIFEST", filepath.Join(tmp, "m.json"))
	t.Setenv("MODEL_DIR", filepath.Join(tmp, "models"))
	t.Setenv("ACTIVE_SLOT_FILE", filepath.Join(tmp, "slot"))
	t.Setenv("UPDATE_STATE", filepath.Join(tmp, "state.json"))
	t.Setenv("NURA_TELEMETRY_FILE", filepath.Join(tmp, "tel.json"))
	t.Setenv("NURA_TELEMETRY", "0")

	h := newHandlers("/dev/null", nil)
	code, body := smokeGET(t, http.HandlerFunc(h.boardHandler), "/board")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	boardRaw, ok := body["board"]
	if !ok || boardRaw == nil {
		t.Fatal("expected board field in response")
	}
}
