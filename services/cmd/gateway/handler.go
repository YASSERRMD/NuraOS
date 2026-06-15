package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/yasserrmd/nuraos/services/internal/agent"
)

// maxChatBodyBytes caps the POST /chat request body to prevent oversized inputs.
const maxChatBodyBytes = 64 * 1024

type handlers struct {
	agentClient *agent.Client
}

func newHandlers(socketPath string) *handlers {
	return &handlers{agentClient: agent.New(socketPath, socketProbeTO)}
}

func (h *handlers) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*socketProbeTO)
	defer cancel()

	agentHealth, err := h.agentClient.Health(ctx)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"status":          "degraded",
			"agent_reachable": false,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":          "ok",
		"agent_reachable": true,
		"agent":           agentHealth,
	})
}

func (h *handlers) version(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"service": "nura-gateway",
		"version": version,
	})
}

func (h *handlers) chat(w http.ResponseWriter, r *http.Request) {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType,
			map[string]string{"error": "Content-Type must be application/json"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxChatBodyBytes)

	var req struct {
		Messages    []agent.Message `json:"messages"`
		MaxTokens   int             `json:"max_tokens,omitempty"`
		Temperature float32         `json:"temperature,omitempty"`
		Provider    string          `json:"provider,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		code := http.StatusBadRequest
		msg := "invalid JSON: " + err.Error()
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			code = http.StatusRequestEntityTooLarge
			msg = "request body exceeds 64 KiB limit"
		}
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	if len(req.Messages) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "messages must not be empty"})
		return
	}

	agentReq := agent.TurnRequest{
		Messages:       req.Messages,
		MaxTokens:      req.MaxTokens,
		Temperature:    req.Temperature,
		ProviderHint:   req.Provider,
		StreamResponse: true,
	}

	// r.Context() is cancelled by the HTTP server on client disconnect.
	// Passing it to ChatStream causes the agent connection to be dropped,
	// which the agent interprets as a turn cancellation.
	ctx := r.Context()
	resp, err := h.agentClient.ChatStream(ctx, agentReq)
	if err != nil {
		slog.Warn("chat: agent unreachable", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent unavailable"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("chat: agent returned non-200", "status", resp.StatusCode)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "agent returned error"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	// Clear the server write deadline so long SSE streams are not cut off.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(zeroTime)

	w.WriteHeader(http.StatusOK)

	// Close the body when the request context is done (client disconnected)
	// so that a blocking resp.Body.Read() returns promptly.
	bodyDone := make(chan struct{})
	defer close(bodyDone)
	go func() {
		select {
		case <-ctx.Done():
			resp.Body.Close()
		case <-bodyDone:
		}
	}()

	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		nr, readErr := resp.Body.Read(buf)
		if nr > 0 {
			if _, werr := w.Write(buf[:nr]); werr != nil {
				return // client disconnected
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr != nil {
			return
		}
	}
}

func (h *handlers) tools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	toolsResp, err := h.agentClient.Tools(ctx)
	if err != nil {
		slog.Warn("tools: agent unreachable", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, toolsResp)
}
