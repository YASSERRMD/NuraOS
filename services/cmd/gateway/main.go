// Package main is the nura-gateway HTTP service.
//
// It fronts the Rust nura-agent for off-box access. Phase 28 ships
// /healthz and /version; Phase 29 adds /chat (SSE) and /tools;
// Phase 30 adds auth, rate limiting, and loopback-only binding;
// Phase 31 adds /metrics (Prometheus) and /status (health summary).
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// version is injected at build time:
// go build -ldflags "-X main.version=v0.1.0" ./cmd/gateway
var version = "dev"

// zeroTime is used to clear HTTP write deadlines for streaming responses.
var zeroTime time.Time

const (
	defaultPort   = "8080"
	agentSocket   = "/run/nura-agent.sock"
	socketProbeTO = 500 * time.Millisecond
)

func main() {
	port := os.Getenv("GATEWAY_PORT")
	if port == "" {
		port = defaultPort
	}

	// Bind policy: loopback-only by default; LAN requires explicit opt-in.
	host := "127.0.0.1"
	if os.Getenv("GATEWAY_BIND_LAN") == "1" {
		host = "0.0.0.0"
		slog.Warn("LAN bind enabled; gateway is accessible from the network")
	}

	// Optional bearer-token auth loaded from secrets file.
	token := loadGatewayToken(defaultSecretsPath)
	if token != "" {
		slog.Info("gateway auth enabled")
	}

	store := newMetricsStore()
	h := newHandlers(agentSocket, store)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /version", h.version)
	mux.HandleFunc("POST /chat", h.chat)
	mux.HandleFunc("GET /tools", h.tools)
	mux.HandleFunc("GET /metrics", h.metricsHandler)
	mux.HandleFunc("GET /status", h.statusHandler)

	rl := newRateLimiter(defaultRPS, defaultBurst)
	sem := make(chan struct{}, maxConcurrent)

	// Middleware chain (outermost first):
	// security headers -> auth -> rate limit -> concurrency cap -> handler
	var handler http.Handler = mux
	handler = concurrencyMiddleware(handler, sem, store)
	handler = rateLimitMiddleware(handler, rl, store)
	handler = bearerAuthMiddleware(handler, token)
	handler = securityHeadersMiddleware(handler)

	addr := host + ":" + port
	slog.Info("nura-gateway starting",
		"addr", addr,
		"version", version,
		"auth_enabled", token != "",
		"max_concurrent", maxConcurrent,
		"rate_rps", defaultRPS,
	)

	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if err := srv.ListenAndServe(); err != nil {
		slog.Error("gateway terminated", "err", err)
		os.Exit(1)
	}
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
