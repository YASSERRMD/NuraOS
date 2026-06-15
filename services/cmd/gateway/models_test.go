package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestModelsEndpointNoManifestNoDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MODEL_MANIFEST", filepath.Join(tmp, "model.json"))
	t.Setenv("MODEL_DIR", filepath.Join(tmp, "models"))

	h := newHandlers("/dev/null", nil)
	req := httptest.NewRequest(http.MethodGet, "/models", nil)
	w := httptest.NewRecorder()
	h.modelsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["active"] != nil {
		t.Errorf("expected active=null when no manifest, got %v", resp["active"])
	}
	avail, ok := resp["available"].([]interface{})
	if !ok {
		t.Fatalf("expected available to be array, got %T", resp["available"])
	}
	if len(avail) != 0 {
		t.Errorf("expected empty available list, got %d entries", len(avail))
	}
}

func TestModelsEndpointWithManifest(t *testing.T) {
	tmp := t.TempDir()
	manifestPath := filepath.Join(tmp, "model.json")
	manifest := `{"name":"test-model","path":"/data/models/test-model.gguf","size_bytes":1000,"size_mb":0,"quantization":"Q4_K_M","context_length":2048,"parameters_billions":1.7,"architecture":"smollm2"}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MODEL_MANIFEST", manifestPath)
	t.Setenv("MODEL_DIR", filepath.Join(tmp, "models"))

	h := newHandlers("/dev/null", nil)
	req := httptest.NewRequest(http.MethodGet, "/models", nil)
	w := httptest.NewRecorder()
	h.modelsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Active json.RawMessage `json:"active"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Active == nil {
		t.Fatal("expected active model in response")
	}
	var active map[string]interface{}
	if err := json.Unmarshal(resp.Active, &active); err != nil {
		t.Fatal(err)
	}
	if active["name"] != "test-model" {
		t.Errorf("expected name=test-model, got %v", active["name"])
	}
}

func TestModelsEndpointAvailableList(t *testing.T) {
	tmp := t.TempDir()
	modelsDir := filepath.Join(tmp, "models")
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create two fake .gguf files and one non-gguf file.
	files := []string{"model-a.gguf", "model-b.gguf", "readme.txt"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(modelsDir, f), []byte("fake"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MODEL_MANIFEST", filepath.Join(tmp, "model.json")) // no manifest
	t.Setenv("MODEL_DIR", modelsDir)

	h := newHandlers("/dev/null", nil)
	req := httptest.NewRequest(http.MethodGet, "/models", nil)
	w := httptest.NewRecorder()
	h.modelsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Available []map[string]interface{} `json:"available"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Available) != 2 {
		t.Fatalf("expected 2 available models (gguf only), got %d", len(resp.Available))
	}
	for _, m := range resp.Available {
		name, _ := m["name"].(string)
		if name != "model-a" && name != "model-b" {
			t.Errorf("unexpected model name: %s", name)
		}
	}
}

func TestModelsMetricsIncrement(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MODEL_MANIFEST", filepath.Join(tmp, "model.json"))
	t.Setenv("MODEL_DIR", filepath.Join(tmp, "models"))

	store := newMetricsStore()
	h := newHandlers("/dev/null", store)
	req := httptest.NewRequest(http.MethodGet, "/models", nil)
	w := httptest.NewRecorder()
	h.modelsHandler(w, req)

	if got := store.reqTotal[epModels].Load(); got != 1 {
		t.Errorf("expected epModels counter=1, got %d", got)
	}
}
