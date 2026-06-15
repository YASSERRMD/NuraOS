package history_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/history"
)

func newStore(t *testing.T) *history.Store {
	t.Helper()
	return history.NewStore(t.TempDir())
}

func TestAddAndList(t *testing.T) {
	s := newStore(t)

	for _, slot := range []string{"a", "b", "a"} {
		if err := s.Add(history.Entry{Slot: slot, ImageVersion: "1.0.0"}, 10); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("len = %d; want 3", len(entries))
	}
	// List returns newest-first.
	if entries[0].Timestamp < entries[1].Timestamp {
		t.Error("list is not sorted newest-first")
	}
}

func TestMarkKnownGood(t *testing.T) {
	s := newStore(t)
	if err := s.Add(history.Entry{Slot: "b", ImageVersion: "2.0.0", ID: "tx42"}, 10); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkKnownGood("tx42"); err != nil {
		t.Fatalf("MarkKnownGood: %v", err)
	}
	e, ok := s.Get("tx42")
	if !ok {
		t.Fatal("entry not found after MarkKnownGood")
	}
	if !e.KnownGood {
		t.Error("KnownGood not set")
	}
}

func TestMarkKnownGoodMissing(t *testing.T) {
	s := newStore(t)
	if err := s.MarkKnownGood("nonexistent"); err == nil {
		t.Fatal("expected error for missing ID, got nil")
	}
}

func TestLatestKnownGood(t *testing.T) {
	s := newStore(t)
	// Use explicit timestamps to avoid second-precision ties.
	s.Add(history.Entry{ID: "old", Slot: "a", KnownGood: false, Timestamp: "2026-01-01T00:00:00Z"}, 10)
	s.Add(history.Entry{ID: "good1", Slot: "b", KnownGood: true, Timestamp: "2026-01-01T00:01:00Z"}, 10)
	s.Add(history.Entry{ID: "good2", Slot: "a", KnownGood: true, Timestamp: "2026-01-01T00:02:00Z"}, 10)
	s.Add(history.Entry{ID: "latest", Slot: "b", KnownGood: false, Timestamp: "2026-01-01T00:03:00Z"}, 10)

	e, ok := s.LatestKnownGood()
	if !ok {
		t.Fatal("expected a known-good entry")
	}
	// good2 has the later timestamp so it should be the latest known-good.
	if e.ID != "good2" {
		t.Errorf("LatestKnownGood.ID = %q; want good2", e.ID)
	}
}

func TestPruneRespectsMaxAndKnownGood(t *testing.T) {
	s := newStore(t)

	// Add 5 normal entries.
	for i := 0; i < 5; i++ {
		s.Add(history.Entry{Slot: "a"}, 10)
	}
	// Add 1 known-good.
	s.Add(history.Entry{ID: "kg", Slot: "b", KnownGood: true}, 10)
	// Add 6 more normal entries (total 12).
	for i := 0; i < 6; i++ {
		s.Add(history.Entry{Slot: "a"}, 10)
	}

	// Prune to 5; the known-good entry should survive.
	if err := s.Prune(5); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	entries, _ := s.List()
	if len(entries) > 5 {
		t.Errorf("after prune to 5: len = %d; want <= 5", len(entries))
	}
	found := false
	for _, e := range entries {
		if e.ID == "kg" {
			found = true
			break
		}
	}
	if !found {
		t.Error("known-good entry was pruned; it should be retained")
	}
}

func TestRetentionOnAdd(t *testing.T) {
	s := newStore(t)
	maxEntries := 3

	// Add 5 entries with a limit of 3.
	for i := 0; i < 5; i++ {
		if err := s.Add(history.Entry{Slot: "a"}, maxEntries); err != nil {
			t.Fatal(err)
		}
	}

	entries, _ := s.List()
	if len(entries) != maxEntries {
		t.Errorf("len after capped adds = %d; want %d", len(entries), maxEntries)
	}
}

func TestHistoryPersists(t *testing.T) {
	dir := t.TempDir()
	s1 := history.NewStore(dir)
	s1.Add(history.Entry{ID: "persist-me", Slot: "b"}, 10)

	s2 := history.NewStore(dir)
	e, ok := s2.Get("persist-me")
	if !ok {
		t.Fatal("entry not found in second store instance")
	}
	if e.Slot != "b" {
		t.Errorf("slot = %q; want b", e.Slot)
	}
}

func TestRollbackWritesActiveSlot(t *testing.T) {
	dir := t.TempDir()

	// Seed active-slot.
	etcDir := filepath.Join(dir, "etc")
	os.MkdirAll(etcDir, 0o755)
	os.WriteFile(filepath.Join(etcDir, "active-slot"), []byte("b\n"), 0o644)

	s := history.NewStore(dir)
	s.Add(history.Entry{ID: "v1", Slot: "a", KnownGood: true}, 10)

	if err := s.RollbackTo("v1", dir); err != nil {
		t.Fatalf("RollbackTo: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(etcDir, "active-slot"))
	got := string(data)
	if got != "a\n" {
		t.Errorf("active-slot = %q; want 'a\\n'", got)
	}
}
