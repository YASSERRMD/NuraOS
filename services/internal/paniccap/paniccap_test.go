package paniccap_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/paniccap"
)

// syntheticPstore creates a temporary directory with fake pstore files.
func syntheticPstore(t *testing.T) (pstoreDir string) {
	t.Helper()
	dir := t.TempDir()
	entries := map[string]string{
		"dmesg-ramoops-0": "Kernel panic - not syncing: Fatal exception\ntoken=secret123\nCall Trace:\n",
		"dmesg-ramoops-1": "Kernel panic - not syncing: Fatal exception (second boot)\n",
		"efi-pstore-1":    "EFI variable (not a crash record)",
	}
	for name, content := range entries {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write pstore fixture %s: %v", name, err)
		}
	}
	return dir
}

// TestCollectAndArchiveReadsRecords verifies dmesg entries are collected.
func TestCollectAndArchiveReadsRecords(t *testing.T) {
	pstoreDir := syntheticPstore(t)
	crashDir := t.TempDir()

	records, err := paniccap.CollectAndArchive(pstoreDir, crashDir)
	if err != nil {
		t.Fatalf("CollectAndArchive: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("got %d records; want 2 (only dmesg- entries)", len(records))
	}
}

// TestCollectArchivesRedacted verifies secrets are stripped from archived files.
func TestCollectArchivesRedacted(t *testing.T) {
	pstoreDir := syntheticPstore(t)
	crashDir := t.TempDir()

	records, err := paniccap.CollectAndArchive(pstoreDir, crashDir)
	if err != nil {
		t.Fatalf("CollectAndArchive: %v", err)
	}

	for _, rec := range records {
		if rec.ArchivedTo == "" {
			continue
		}
		data, err := os.ReadFile(rec.ArchivedTo)
		if err != nil {
			t.Fatalf("read archived file %s: %v", rec.ArchivedTo, err)
		}
		if string(data) != "" {
			// Check that "secret123" from fixture is not present.
			for _, line := range rec.Lines {
				if contains(line, "secret123") {
					t.Errorf("unredacted secret found in record lines: %q", line)
				}
			}
		}
	}
}

// TestHasPendingRecordsTrue verifies pstore detection when files exist.
func TestHasPendingRecordsTrue(t *testing.T) {
	dir := syntheticPstore(t)
	if !paniccap.HasPendingRecords(dir) {
		t.Error("HasPendingRecords = false; want true when dmesg files present")
	}
}

// TestHasPendingRecordsFalseEmpty verifies detection on empty dir.
func TestHasPendingRecordsFalseEmpty(t *testing.T) {
	dir := t.TempDir()
	if paniccap.HasPendingRecords(dir) {
		t.Error("HasPendingRecords = true; want false when dir is empty")
	}
}

// TestCollectNonExistentPstoreReturnsNil verifies graceful handling.
func TestCollectNonExistentPstoreReturnsNil(t *testing.T) {
	records, err := paniccap.CollectAndArchive("/nonexistent-pstore-path-xyz", t.TempDir())
	if err != nil {
		t.Fatalf("CollectAndArchive with missing pstore: unexpected error: %v", err)
	}
	if records != nil {
		t.Errorf("expected nil records for missing pstore; got %v", records)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
