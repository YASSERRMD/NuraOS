// Package main is the nura-gateway HTTP service.
//
// It fronts the Rust nura-agent for off-box access. Phase 28 ships
// /healthz and /version; Phase 29 adds /chat (SSE) and /tools.
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

	h := newHandlers(agentSocket)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /version", h.version)
	mux.HandleFunc("POST /chat", h.chat)
	mux.HandleFunc("GET /tools", h.tools)

	addr := "0.0.0.0:" + port
	slog.Info("nura-gateway starting", "addr", addr, "version", version)

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
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
