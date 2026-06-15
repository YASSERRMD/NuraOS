// Package main is the nura-gateway HTTP service.
//
// It fronts the Rust nura-agent for off-box access. Phase 28 ships
// /healthz and /version; subsequent phases add /chat and /tools.
package main

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"
)

// version is injected at build time:
// go build -ldflags "-X main.version=v0.1.0" ./cmd/gateway
var version = "dev"

const (
	defaultPort = "8080"
	// agentSocket is the Unix domain socket the Rust agent listens on.
	// Defined here so all gateway packages use the same constant.
	agentSocket    = "/run/nura-agent.sock"
	socketProbeTO  = 500 * time.Millisecond
)

func main() {
	port := os.Getenv("GATEWAY_PORT")
	if port == "" {
		port = defaultPort
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /version", handleVersion)

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

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	agentReachable := probeAgentSocket()
	body := map[string]interface{}{
		"status":          "ok",
		"agent_reachable": agentReachable,
	}
	code := http.StatusOK
	if !agentReachable {
		body["status"] = "degraded"
		// Return 503 so load-balancers can detect a degraded instance.
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, body)
}

func handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"service": "nura-gateway",
		"version": version,
	})
}

// probeAgentSocket dials the agent's Unix socket to confirm it is accepting
// connections. A failure means the agent has not yet started or has crashed.
func probeAgentSocket() bool {
	conn, err := net.DialTimeout("unix", agentSocket, socketProbeTO)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
