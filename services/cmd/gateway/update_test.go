package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateStatusDefaultSlot(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ACTIVE_SLOT_FILE", filepath.Join(tmp, "active-slot"))
	t.Setenv("UPDATE_STATE", filepath.Join(tmp, "update-state.json"))

	h := newHandlers("/dev/null", nil)
	req := httptest.NewRequest(http.MethodGet, "/update/status", nil)
	w := httptest.NewRecorder()
	h.updateStatusHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	// When slot file is absent the handler defaults to slot "a".
	if resp["active_slot"] != "a" {
		t.Errorf("expected default active_slot=a, got %v", resp["active_slot"])
	}
	if resp["inactive_slot"] != "b" {
		t.Errorf("expected inactive_slot=b, got %v", resp["inactive_slot"])
	}
	if resp["update_state"] != nil {
		t.Errorf("expected update_state=null with no state file, got %v", resp["update_state"])
	}
}

func TestUpdateStatusSlotB(t *testing.T) {
	tmp := t.TempDir()
	slotFile := filepath.Join(tmp, "active-slot")
	if err := os.WriteFile(slotFile, []byte("b\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIVE_SLOT_FILE", slotFile)
	t.Setenv("UPDATE_STATE", filepath.Join(tmp, "update-state.json"))

	h := newHandlers("/dev/null", nil)
	req := httptest.NewRequest(http.MethodGet, "/update/status", nil)
	w := httptest.NewRecorder()
	h.updateStatusHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["active_slot"] != "b" {
		t.Errorf("expected active_slot=b, got %v", resp["active_slot"])
	}
	if resp["inactive_slot"] != "a" {
		t.Errorf("expected inactive_slot=a, got %v", resp["inactive_slot"])
	}
}

func TestUpdateStatusWithStateFile(t *testing.T) {
	tmp := t.TempDir()
	slotFile := filepath.Join(tmp, "active-slot")
	if err := os.WriteFile(slotFile, []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(tmp, "update-state.json")
	stateJSON := `{"active_slot":"a","pending_slot":"b","last_update":"2026-01-01T00:00:00Z","last_result":"pending_reboot","boot_attempts":0}`
	if err := os.WriteFile(stateFile, []byte(stateJSON), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIVE_SLOT_FILE", slotFile)
	t.Setenv("UPDATE_STATE", stateFile)

	h := newHandlers("/dev/null", nil)
	req := httptest.NewRequest(http.MethodGet, "/update/status", nil)
	w := httptest.NewRecorder()
	h.updateStatusHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		ActiveSlot   string          `json:"active_slot"`
		InactiveSlot string          `json:"inactive_slot"`
		UpdateState  json.RawMessage `json:"update_state"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ActiveSlot != "a" {
		t.Errorf("expected active_slot=a, got %s", resp.ActiveSlot)
	}
	if resp.UpdateState == nil {
		t.Fatal("expected update_state to be populated")
	}
	var state map[string]interface{}
	if err := json.Unmarshal(resp.UpdateState, &state); err != nil {
		t.Fatal(err)
	}
	if state["last_result"] != "pending_reboot" {
		t.Errorf("expected last_result=pending_reboot, got %v", state["last_result"])
	}
}

func TestUpdateStatusMetricsIncrement(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ACTIVE_SLOT_FILE", filepath.Join(tmp, "active-slot"))
	t.Setenv("UPDATE_STATE", filepath.Join(tmp, "update-state.json"))

	store := newMetricsStore()
	h := newHandlers("/dev/null", store)
	req := httptest.NewRequest(http.MethodGet, "/update/status", nil)
	w := httptest.NewRecorder()
	h.updateStatusHandler(w, req)

	if got := store.reqTotal[epUpdateStatus].Load(); got != 1 {
		t.Errorf("expected epUpdateStatus counter=1, got %d", got)
	}
}
