package compliance

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DeleteResult describes the outcome of a data-deletion run.
type DeleteResult struct {
	// SessionsDeleted is the count of session files removed.
	SessionsDeleted int `json:"sessions_deleted"`
	// JournalEntriesDeleted is the count of journal files removed.
	JournalEntriesDeleted int `json:"journal_entries_deleted"`
	// ProvenanceRecordsDeleted is the count of provenance files removed.
	ProvenanceRecordsDeleted int `json:"provenance_records_deleted"`
	// BytesFreed is the total bytes freed.
	BytesFreed int64 `json:"bytes_freed"`
	// Errors lists non-fatal errors encountered during deletion.
	Errors []string `json:"errors,omitempty"`
}

// DeleteExpired removes data files older than retentionDays from dataDir.
// It targets /data/sessions, /data/journal, and /data/provenance subdirectories.
// Pass retentionDays=0 to use the policy default (90 days).
func DeleteExpired(dataDir string, retentionDays int) (DeleteResult, error) {
	if retentionDays <= 0 {
		retentionDays = 90
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	var res DeleteResult

	targets := []struct {
		subdir  string
		counter *int
	}{
		{"sessions", &res.SessionsDeleted},
		{"journal", &res.JournalEntriesDeleted},
		{"provenance", &res.ProvenanceRecordsDeleted},
	}

	for _, t := range targets {
		dir := filepath.Join(dataDir, t.subdir)
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("read %s: %v", dir, err))
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				path := filepath.Join(dir, e.Name())
				size := info.Size()
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					res.Errors = append(res.Errors, fmt.Sprintf("remove %s: %v", path, err))
					continue
				}
				(*t.counter)++
				res.BytesFreed += size
			}
		}
	}

	if len(res.Errors) > 0 {
		return res, fmt.Errorf("compliance: %d errors during deletion", len(res.Errors))
	}
	return res, nil
}
