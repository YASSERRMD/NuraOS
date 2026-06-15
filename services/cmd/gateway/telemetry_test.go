package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildPayloadFields(t *testing.T) {
	store := newMetricsStore()
	store.chatLatCount.Add(7)

	p := buildPayload(store)

	if p.Event != telemetryEvent {
		t.Errorf("expected event=%s, got %s", telemetryEvent, p.Event)
	}
	if p.Version == "" {
		t.Error("expected non-empty version")
	}
	if p.TurnsTotal != 7 {
		t.Errorf("expected turns_total=7, got %d", p.TurnsTotal)
	}
	if p.UptimeSeconds < 0 {
		t.Errorf("expected non-negative uptime, got %d", p.UptimeSeconds)
	}
	if p.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
}

func TestBuildPayloadModelFromManifest(t *testing.T) {
	tmp := t.TempDir()
	manifest := filepath.Join(tmp, "model.json")
	if err := os.WriteFile(manifest, []byte(`{"name":"test-model-q4"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MODEL_MANIFEST", manifest)

	store := newMetricsStore()
	p := buildPayload(store)

	if p.Model != "test-model-q4" {
		t.Errorf("expected model=test-model-q4, got %q", p.Model)
	}
}

func TestExportTelemetryLocalOnly(t *testing.T) {
	tmp := t.TempDir()
	localFile := filepath.Join(tmp, "telemetry.json")
	t.Setenv("NURA_TELEMETRY_FILE", localFile)

	payload := telemetryPayload{
		Event:         telemetryEvent,
		Version:       "test",
		UptimeSeconds: 60,
		TurnsTotal:    3,
		Timestamp:     "2026-01-01T00:00:00Z",
	}

	result := exportTelemetry(payload, localFile, "")
	if result != "local_only" {
		t.Errorf("expected local_only, got %s", result)
	}
	if _, err := os.Stat(localFile); err != nil {
		t.Fatalf("expected local file to be written: %v", err)
	}
	var written telemetryPayload
	data, _ := os.ReadFile(localFile)
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatal(err)
	}
	if written.TurnsTotal != 3 {
		t.Errorf("expected turns_total=3 in written file, got %d", written.TurnsTotal)
	}
}

func TestExportTelemetryRemoteURL(t *testing.T) {
	received := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf [4096]byte
		n, _ := r.Body.Read(buf[:])
		received <- buf[:n]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tmp := t.TempDir()
	localFile := filepath.Join(tmp, "telemetry.json")

	payload := telemetryPayload{
		Event:   telemetryEvent,
		Version: "test",
	}

	result := exportTelemetry(payload, localFile, srv.URL)
	if result != "ok" {
		t.Errorf("expected ok, got %s", result)
	}

	select {
	case body := <-received:
		var p telemetryPayload
		if err := json.Unmarshal(body, &p); err != nil {
			t.Fatalf("server received invalid JSON: %v", err)
		}
		if p.Event != telemetryEvent {
			t.Errorf("expected event=%s, got %s", telemetryEvent, p.Event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("remote server did not receive telemetry payload")
	}
}

func TestTelemetryStatusHandlerDisabled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("NURA_TELEMETRY", "0")
	t.Setenv("NURA_TELEMETRY_FILE", filepath.Join(tmp, "telemetry.json"))

	h := newHandlers("/dev/null", nil)
	req := httptest.NewRequest(http.MethodGet, "/telemetry/status", nil)
	w := httptest.NewRecorder()
	h.telemetryStatusHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Telemetry   map[string]interface{} `json:"telemetry"`
		LastPayload json.RawMessage        `json:"last_payload"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Telemetry["enabled"] != false {
		t.Errorf("expected enabled=false, got %v", resp.Telemetry["enabled"])
	}
	if len(resp.LastPayload) > 0 && string(resp.LastPayload) != "null" {
		t.Errorf("expected last_payload=null, got %s", resp.LastPayload)
	}
}

func TestTelemetryStatusHandlerWithLastPayload(t *testing.T) {
	tmp := t.TempDir()
	localFile := filepath.Join(tmp, "telemetry.json")
	payload := `{"event":"heartbeat","version":"test","uptime_seconds":120,"turns_total":5}`
	if err := os.WriteFile(localFile, []byte(payload), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NURA_TELEMETRY", "1")
	t.Setenv("NURA_TELEMETRY_FILE", localFile)

	h := newHandlers("/dev/null", nil)
	req := httptest.NewRequest(http.MethodGet, "/telemetry/status", nil)
	w := httptest.NewRecorder()
	h.telemetryStatusHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Telemetry   map[string]interface{} `json:"telemetry"`
		LastPayload json.RawMessage        `json:"last_payload"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Telemetry["enabled"] != true {
		t.Errorf("expected enabled=true, got %v", resp.Telemetry["enabled"])
	}
	if resp.LastPayload == nil {
		t.Fatal("expected last_payload to be populated")
	}
}

func TestTelemetryMetricsIncrement(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("NURA_TELEMETRY_FILE", filepath.Join(tmp, "telemetry.json"))

	store := newMetricsStore()
	h := newHandlers("/dev/null", store)
	req := httptest.NewRequest(http.MethodGet, "/telemetry/status", nil)
	w := httptest.NewRecorder()
	h.telemetryStatusHandler(w, req)

	if got := store.reqTotal[epTelemetryStatus].Load(); got != 1 {
		t.Errorf("expected epTelemetryStatus=1, got %d", got)
	}
}
