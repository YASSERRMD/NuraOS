// Package main is the nura-gateway HTTP service.
//
// It fronts the Rust nura-agent for off-box access. Phase 28 ships
// /healthz and /version; Phase 29 adds /chat (SSE) and /tools;
// Phase 30 adds auth, rate limiting, and loopback-only binding;
// Phase 31 adds /metrics (Prometheus) and /status (health summary);
// Phase 34 adds graceful shutdown on SIGTERM/SIGINT.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	_ "net/http/pprof" // registers pprof handlers on http.DefaultServeMux
	"os"
	"os/signal"
	"syscall"
	"time"
)

// version is injected at build time:
// go build -ldflags "-X main.version=v0.1.0" ./cmd/gateway
var version = "dev"

// zeroTime is used to clear HTTP write deadlines for streaming responses.
var zeroTime time.Time

const (
	defaultPort      = "8080"
	agentSocket      = "/run/nura-agent.sock"
	socketProbeTO    = 500 * time.Millisecond
	shutdownTimeout  = 15 * time.Second
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

	// Bearer-token auth: load from secrets file; reload on SIGHUP.
	ts := newTokenStore(defaultSecretsPath)
	ts.watchSIGHUP()
	if ts.get() != "" {
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
	handler = bearerAuthMiddleware(handler, ts)
	handler = securityHeadersMiddleware(handler)

	addr := host + ":" + port
	slog.Info("nura-gateway starting",
		"addr", addr,
		"version", version,
		"auth_enabled", ts.get() != "",
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

	// Graceful shutdown: listen for SIGTERM or SIGINT, then drain connections.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		slog.Info("shutdown signal received; draining connections",
			"signal", sig,
			"timeout", shutdownTimeout,
		)
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("graceful shutdown did not complete within timeout",
				"err", err,
				"timeout", shutdownTimeout,
			)
		}
	}()

	// Optional pprof profiling endpoint: NURA_PPROF=1 starts a loopback-only
	// HTTP server on port 6060 exposing /debug/pprof/* (no auth).
	if os.Getenv("NURA_PPROF") == "1" {
		pprofAddr := "127.0.0.1:6060"
		pprofSrv := &http.Server{
			Addr:         pprofAddr,
			Handler:      http.DefaultServeMux,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 60 * time.Second,
		}
		go func() {
			slog.Info("pprof endpoint active", "addr", pprofAddr)
			if err := pprofSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("pprof server error", "err", err)
			}
		}()
	}

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("gateway terminated", "err", err)
		os.Exit(1)
	}
	slog.Info("gateway shutdown complete")
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
