package diskmon

import (
	"os"
	"path/filepath"
)

// Quota describes a soft size cap for a directory subtree.
type Quota struct {
	// Path is the directory to measure.
	Path string
	// MaxBytes is the soft cap; 0 means unlimited.
	MaxBytes int64
}

// SubtreeUsage returns the total byte size of all regular files under dir.
// Errors reading individual entries are skipped (best-effort walk).
func SubtreeUsage(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// Check reports how many bytes are in use and whether the subtree is within quota.
// When MaxBytes is 0, ok is always true.
// Read errors are treated as within-quota (permissive on failure).
func (q Quota) Check() (used int64, ok bool, err error) {
	if q.MaxBytes <= 0 {
		return 0, true, nil
	}
	used, err = SubtreeUsage(q.Path)
	if err != nil {
		return 0, true, err
	}
	return used, used <= q.MaxBytes, nil
}
