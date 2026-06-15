package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultTelemetryInterval = 30 * time.Minute
	telemetryEvent           = "heartbeat"
)

// telemetryPayload is the privacy-preserving event sent or written locally.
// It contains only aggregate, non-PII counters and public identifiers.
type telemetryPayload struct {
	Event         string `json:"event"`
	Version       string `json:"version"`
	Model         string `json:"model,omitempty"`
	Provider      string `json:"provider,omitempty"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	TurnsTotal    int64  `json:"turns_total"`
	Timestamp     string `json:"timestamp"`
}

// telemetryState tracks the last export for GET /telemetry/status.
type telemetryState struct {
	Enabled    bool   `json:"enabled"`
	RemoteURL  string `json:"remote_url,omitempty"`
	LocalFile  string `json:"local_file"`
	LastSentAt string `json:"last_sent_at,omitempty"`
	LastResult string `json:"last_result,omitempty"`
}

func telemetryEnabled() bool {
	return os.Getenv("NURA_TELEMETRY") == "1"
}

func telemetryRemoteURL() string {
	return os.Getenv("NURA_TELEMETRY_URL")
}

func telemetryLocalFile() string {
	if v := os.Getenv("NURA_TELEMETRY_FILE"); v != "" {
		return v
	}
	return "/data/etc/telemetry.json"
}

// buildPayload constructs the telemetry event from live gateway state.
// Model name is read from the model manifest (MODEL_MANIFEST env or default).
// Provider name is read from the agent metrics if available.
func buildPayload(store *MetricsStore) telemetryPayload {
	p := telemetryPayload{
		Event:         telemetryEvent,
		Version:       version,
		UptimeSeconds: store.uptimeSeconds(),
		TurnsTotal:    store.chatLatCount.Load(),
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}

	// Model name from the active manifest (non-PII: public model identifier).
	if data, err := os.ReadFile(modelManifestPath()); err == nil {
		var m struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(data, &m); err == nil && m.Name != "" {
			p.Model = m.Name
		}
	}

	return p
}

// exportTelemetry writes the payload locally and, if configured, POSTs to the
// remote URL. All exported data is written to the local file regardless so
// the user can audit what would be sent.
func exportTelemetry(payload telemetryPayload, localFile, remoteURL string) string {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "marshal_error"
	}

	// Always write locally first.
	if dir := localFile[:strings.LastIndex(localFile, "/")+1]; dir != "" && dir != "/" {
		_ = os.MkdirAll(dir, 0755)
	}
	if werr := os.WriteFile(localFile, body, 0644); werr != nil {
		slog.Warn("telemetry: could not write local file", "path", localFile, "err", werr)
	}

	if remoteURL == "" {
		return "local_only"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, remoteURL, bytes.NewReader(body))
	if err != nil {
		return "request_build_error"
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "nura-gateway/"+version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("telemetry: remote export failed", "err", err)
		return "remote_error"
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return "ok"
	}
	return "remote_http_" + http.StatusText(resp.StatusCode)
}

// startTelemetryLoop runs in a background goroutine when NURA_TELEMETRY=1.
// It fires immediately on start and then every interval.
func startTelemetryLoop(ctx context.Context, store *MetricsStore, interval time.Duration) {
	localFile := telemetryLocalFile()
	remoteURL := telemetryRemoteURL()

	do := func() {
		payload := buildPayload(store)
		result := exportTelemetry(payload, localFile, remoteURL)
		slog.Info("telemetry exported", "result", result, "remote", remoteURL != "")
	}

	do()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			do()
		}
	}
}
