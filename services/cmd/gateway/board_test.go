package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestBoardHandlerNoBoardFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("BOARD_INFO_FILE", filepath.Join(tmp, "board.json"))

	h := newHandlers("/dev/null", nil)
	req := httptest.NewRequest(http.MethodGet, "/board", nil)
	w := httptest.NewRecorder()
	h.boardHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	// board should be null when file is absent
	if resp["board"] != nil {
		t.Errorf("expected board=null with no file, got %v", resp["board"])
	}
}

func TestBoardHandlerWithBoardFile(t *testing.T) {
	tmp := t.TempDir()
	boardFile := filepath.Join(tmp, "board.json")
	boardJSON := `{"id":"qemu-x86_64","name":"QEMU x86-64 (development / CI)","arch":"x86_64","min_ram_mb":512}`
	if err := os.WriteFile(boardFile, []byte(boardJSON), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOARD_INFO_FILE", boardFile)

	h := newHandlers("/dev/null", nil)
	req := httptest.NewRequest(http.MethodGet, "/board", nil)
	w := httptest.NewRecorder()
	h.boardHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Board json.RawMessage `json:"board"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Board == nil {
		t.Fatal("expected board to be populated")
	}
	var board map[string]interface{}
	if err := json.Unmarshal(resp.Board, &board); err != nil {
		t.Fatal(err)
	}
	if board["id"] != "qemu-x86_64" {
		t.Errorf("expected board id=qemu-x86_64, got %v", board["id"])
	}
	if board["arch"] != "x86_64" {
		t.Errorf("expected arch=x86_64, got %v", board["arch"])
	}
}

func TestBoardMetricsIncrement(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("BOARD_INFO_FILE", filepath.Join(tmp, "board.json"))

	store := newMetricsStore()
	h := newHandlers("/dev/null", store)
	req := httptest.NewRequest(http.MethodGet, "/board", nil)
	w := httptest.NewRecorder()
	h.boardHandler(w, req)

	if got := store.reqTotal[epBoard].Load(); got != 1 {
		t.Errorf("expected epBoard counter=1, got %d", got)
	}
}
