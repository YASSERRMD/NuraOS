// Package paniccap reads kernel panic records from pstore (EFI/ramoops) and
// from the kernel log ring buffer so that post-mortem analysis is possible
// after an unplanned reboot.
//
// On first boot after a panic, pstore entries are present under
// /sys/fs/pstore/. The package reads, redacts, and archives them to
// /data/crashes so they survive across reboots, then optionally clears them
// so the next boot starts clean.
package paniccap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/crashcap"
)

const (
	// DefaultPstoreDir is the standard Linux pstore mount point.
	DefaultPstoreDir = "/sys/fs/pstore"
	// DefaultDmesgPath is the kernel ring buffer device for dmesg capture.
	DefaultDmesgPath = "/dev/kmsg"
)

// PanicRecord describes one kernel panic entry read from pstore.
type PanicRecord struct {
	// ID is the pstore filename (e.g. "dmesg-ramoops-0").
	ID string `json:"id"`
	// CapturedAt is when this package read the record.
	CapturedAt time.Time `json:"captured_at"`
	// Lines are the redacted log lines from the pstore entry.
	Lines []string `json:"lines"`
	// ArchivedTo is the path in /data/crashes where this record was saved.
	ArchivedTo string `json:"archived_to,omitempty"`
}

// CollectAndArchive reads all pstore crash entries, redacts them, archives
// them to crashDir, and returns the records found. It does not clear pstore
// entries (call ClearPstore explicitly if desired).
//
// Returns nil if pstore is not mounted or contains no crash entries.
func CollectAndArchive(pstoreDir, crashDir string) ([]PanicRecord, error) {
	if pstoreDir == "" {
		pstoreDir = DefaultPstoreDir
	}
	if crashDir == "" {
		crashDir = crashcap.DefaultCrashDir
	}

	entries, err := os.ReadDir(pstoreDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // pstore not mounted
		}
		return nil, fmt.Errorf("paniccap: read pstore %s: %w", pstoreDir, err)
	}

	if err := os.MkdirAll(crashDir, 0750); err != nil {
		return nil, fmt.Errorf("paniccap: mkdir %s: %w", crashDir, err)
	}

	var records []PanicRecord
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Only process dmesg and panic records; skip EFI variable entries.
		if !strings.HasPrefix(name, "dmesg-") && !strings.HasPrefix(name, "panic-") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(pstoreDir, name))
		if err != nil {
			continue
		}

		redacted := crashcap.RedactBytes(data)
		lines := strings.Split(strings.TrimRight(string(redacted), "\n"), "\n")

		rec := PanicRecord{
			ID:         name,
			CapturedAt: time.Now().UTC(),
			Lines:      lines,
		}

		// Archive to crashDir.
		ts := rec.CapturedAt.Format("20060102T150405Z")
		outName := fmt.Sprintf("kernel-panic-%s-%s.txt", name, ts)
		outPath := filepath.Join(crashDir, outName)
		if err := os.WriteFile(outPath, redacted, 0640); err == nil {
			rec.ArchivedTo = outPath
		}

		records = append(records, rec)
	}
	return records, nil
}

// ClearPstore removes all pstore entries by deleting the files under pstoreDir.
// Call this after successfully archiving records to /data/crashes.
func ClearPstore(pstoreDir string) error {
	if pstoreDir == "" {
		pstoreDir = DefaultPstoreDir
	}
	entries, err := os.ReadDir(pstoreDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("paniccap: clear pstore %s: %w", pstoreDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			_ = os.Remove(filepath.Join(pstoreDir, e.Name()))
		}
	}
	return nil
}

// HasPendingRecords reports whether pstore contains any unarchived crash
// entries. It is a fast probe: it returns true on the first matching entry.
func HasPendingRecords(pstoreDir string) bool {
	if pstoreDir == "" {
		pstoreDir = DefaultPstoreDir
	}
	entries, err := os.ReadDir(pstoreDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, "dmesg-") || strings.HasPrefix(n, "panic-") {
			return true
		}
	}
	return false
}
