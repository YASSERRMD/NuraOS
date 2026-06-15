package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfigEndpointShape(t *testing.T) {
	socketPath, stop := startFakeAgent(t, fakeAgentMux())
	defer stop()

	store := newMetricsStore()
	h := newHandlers(socketPath, store)

	req := httptest.NewRequest(http.MethodGet, "/config", nil)
	rr := httptest.NewRecorder()
	h.configHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%s", rr.Code, rr.Body.String())
	}

	var out struct {
		Gateway struct {
			Version       string  `json:"version"`
			Port          string  `json:"port"`
			Bind          string  `json:"bind"`
			AuthEnabled   bool    `json:"auth_enabled"`
			RateRPS       float64 `json:"rate_rps"`
			RateBurst     float64 `json:"rate_burst"`
			MaxConcurrent int     `json:"max_concurrent"`
			PprofEnabled  bool    `json:"pprof_enabled"`
		} `json:"gateway"`
		Agent struct {
			Socket string `json:"socket"`
		} `json:"agent"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("JSON decode: %v", err)
	}

	if out.Gateway.Version == "" {
		t.Error("gateway.version must not be empty")
	}
	if out.Gateway.Port == "" {
		t.Error("gateway.port must not be empty")
	}
	if out.Gateway.MaxConcurrent <= 0 {
		t.Errorf("gateway.max_concurrent must be > 0, got %d", out.Gateway.MaxConcurrent)
	}
	if out.Agent.Socket == "" {
		t.Error("agent.socket must not be empty")
	}
}

func TestConfigAuthEnabledReported(t *testing.T) {
	socketPath, stop := startFakeAgent(t, fakeAgentMux())
	defer stop()

	store := newMetricsStore()
	h := newHandlers(socketPath, store)
	h.ts = staticToken("mysecret")

	req := httptest.NewRequest(http.MethodGet, "/config", nil)
	rr := httptest.NewRecorder()
	h.configHandler(rr, req)

	var out struct {
		Gateway struct {
			AuthEnabled bool `json:"auth_enabled"`
		} `json:"gateway"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("JSON decode: %v", err)
	}
	if !out.Gateway.AuthEnabled {
		t.Error("want auth_enabled=true when token is set")
	}
}

func TestConfigMetricsIncrement(t *testing.T) {
	socketPath, stop := startFakeAgent(t, fakeAgentMux())
	defer stop()

	store := newMetricsStore()
	h := newHandlers(socketPath, store)

	req := httptest.NewRequest(http.MethodGet, "/config", nil)
	rr := httptest.NewRecorder()
	h.configHandler(rr, req)

	if store.reqTotal[epConfig].Load() != 1 {
		t.Errorf("config request counter: want 1, got %d", store.reqTotal[epConfig].Load())
	}
}
