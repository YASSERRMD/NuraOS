package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/agent"
)

// chatBufPool recycles 4 KiB read buffers across SSE proxy iterations.
var chatBufPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 4096)
		return &b
	},
}

// maxChatBodyBytes caps the POST /chat request body to prevent oversized inputs.
const maxChatBodyBytes = 64 * 1024

type handlers struct {
	agentClient *agent.Client
	store       *MetricsStore
	ts          *tokenStore // nil when auth is disabled (tests)
}

func newHandlers(socketPath string, store *MetricsStore) *handlers {
	return &handlers{
		agentClient: agent.New(socketPath, socketProbeTO),
		store:       store,
	}
}

func (h *handlers) authEnabled() bool {
	return h.ts != nil && h.ts.get() != ""
}

func (h *handlers) healthz(w http.ResponseWriter, r *http.Request) {
	h.store.incRequest(epHealthz)
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
	h.store.incRequest(epVersion)
	writeJSON(w, http.StatusOK, map[string]string{
		"service": "nura-gateway",
		"version": version,
	})
}

func (h *handlers) chat(w http.ResponseWriter, r *http.Request) {
	h.store.incRequest(epChat)
	start := time.Now()

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
	bufp := chatBufPool.Get().(*[]byte)
	buf := *bufp
	defer chatBufPool.Put(bufp)
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
			h.store.recordChatLatency(time.Since(start))
			return
		}
	}
}

func (h *handlers) tools(w http.ResponseWriter, r *http.Request) {
	h.store.incRequest(epTools)
	ctx := r.Context()
	toolsResp, err := h.agentClient.Tools(ctx)
	if err != nil {
		slog.Warn("tools: agent unreachable", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, toolsResp)
}

// metricsHandler serves GET /metrics in Prometheus text exposition format.
// Agent metrics are fetched from the agent socket and appended; if the agent
// is unreachable only gateway-native counters are emitted.
func (h *handlers) metricsHandler(w http.ResponseWriter, r *http.Request) {
	h.store.incRequest(epMetrics)

	ctx, cancel := context.WithTimeout(r.Context(), 2*socketProbeTO)
	defer cancel()

	agentMet, err := h.agentClient.Metrics(ctx)
	var agentMetPtr *agent.AgentMetrics
	if err == nil {
		agentMetPtr = &agentMet
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	h.store.WriteTo(w, agentMetPtr)
}

// configHandler serves GET /config and returns the effective gateway
// configuration. Values reflect environment variables and compiled-in defaults.
// No secrets are included in the response.
func (h *handlers) configHandler(w http.ResponseWriter, r *http.Request) {
	h.store.incRequest(epConfig)

	port := os.Getenv("GATEWAY_PORT")
	if port == "" {
		port = defaultPort
	}
	bind := "127.0.0.1"
	if os.Getenv("GATEWAY_BIND_LAN") == "1" {
		bind = "0.0.0.0"
	}

	type gwConf struct {
		Version       string  `json:"version"`
		Port          string  `json:"port"`
		Bind          string  `json:"bind"`
		AuthEnabled   bool    `json:"auth_enabled"`
		RateRPS       float64 `json:"rate_rps"`
		RateBurst     float64 `json:"rate_burst"`
		MaxConcurrent int     `json:"max_concurrent"`
		PprofEnabled  bool    `json:"pprof_enabled"`
	}
	type agentConf struct {
		Socket string `json:"socket"`
	}

	writeJSON(w, http.StatusOK, struct {
		Gateway gwConf    `json:"gateway"`
		Agent   agentConf `json:"agent"`
	}{
		Gateway: gwConf{
			Version:       version,
			Port:          port,
			Bind:          bind,
			AuthEnabled:   h.authEnabled(),
			RateRPS:       defaultRPS,
			RateBurst:     defaultBurst,
			MaxConcurrent: maxConcurrent,
			PprofEnabled:  os.Getenv("NURA_PPROF") == "1",
		},
		Agent: agentConf{
			Socket: agentSocket,
		},
	})
}

const (
	defaultModelManifestPath = "/data/model.json"
	defaultModelDir          = "/data/models"
)

func modelManifestPath() string {
	if v := os.Getenv("MODEL_MANIFEST"); v != "" {
		return v
	}
	return defaultModelManifestPath
}

func modelDirPath() string {
	if v := os.Getenv("MODEL_DIR"); v != "" {
		return v
	}
	return defaultModelDir
}

// modelEntry describes a GGUF file found in the models directory.
type modelEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	SizeMB    int64  `json:"size_mb"`
}

// modelsHandler serves GET /models.
// It returns the active model manifest and a list of available .gguf files.
// No agent IPC is required; the handler reads the filesystem directly.
func (h *handlers) modelsHandler(w http.ResponseWriter, r *http.Request) {
	h.store.incRequest(epModels)

	manifestPath := modelManifestPath()
	dir := modelDirPath()

	// Read active model manifest (may not exist on a fresh install).
	var active json.RawMessage
	if data, err := os.ReadFile(manifestPath); err == nil {
		active = json.RawMessage(data)
	}

	// List available .gguf files in the models directory.
	available := []modelEntry{}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if len(name) < 5 || name[len(name)-5:] != ".gguf" {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			fullPath := dir + "/" + name
			available = append(available, modelEntry{
				Name:      name[:len(name)-5],
				Path:      fullPath,
				SizeBytes: info.Size(),
				SizeMB:    info.Size() / 1024 / 1024,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active":    active,
		"available": available,
	})
}

// statusHandler serves GET /status with a human-readable JSON health summary.
// Returns 200 when all components are ok; 503 when any component is degraded.
func (h *handlers) statusHandler(w http.ResponseWriter, r *http.Request) {
	h.store.incRequest(epStatus)

	ctx, cancel := context.WithTimeout(r.Context(), 2*socketProbeTO)
	defer cancel()

	components := []agent.StatusComponent{
		{Name: "gateway", Status: "ok", Detail: "version " + version},
	}

	agentComp := agent.StatusComponent{Name: "agent"}
	agentHealth, err := h.agentClient.Health(ctx)
	if err != nil {
		agentComp.Status = "degraded"
		agentComp.Detail = "unreachable"
	} else {
		agentComp.Status = agentHealth.Status
		if agentHealth.Provider != "" {
			agentComp.Detail = "provider=" + agentHealth.Provider
		}
	}
	components = append(components, agentComp)

	overall := "ok"
	for _, c := range components {
		if c.Status != "ok" {
			overall = "degraded"
			break
		}
	}

	resp := agent.StatusResponse{
		Overall:    overall,
		Version:    version,
		Uptime:     h.store.uptimeSeconds(),
		Components: components,
	}

	code := http.StatusOK
	if overall != "ok" {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, resp)
}
