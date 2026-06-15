package diskmon

import (
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ReclaimOptions controls which subtrees are trimmed and their target sizes.
type ReclaimOptions struct {
	// DataDir is the root of the persistent partition (typically "/data").
	DataDir string
	// SessionCap, when > 0, trims the sessions subtree to at most this many bytes
	// by deleting the oldest session files first.
	SessionCap int64
	// LogsCap, when > 0, trims the logs subtree to at most this many bytes.
	LogsCap int64
}

// Reclaim frees space in reclaimable subtrees by removing the oldest files
// until each subtree is at or below its configured cap.
// Returns the total bytes freed across all subtrees.
func Reclaim(opts ReclaimOptions) (int64, error) {
	var freed int64
	if opts.SessionCap > 0 {
		n, _ := trimOldestFiles(filepath.Join(opts.DataDir, "sessions"), opts.SessionCap)
		freed += n
	}
	if opts.LogsCap > 0 {
		n, _ := trimOldestFiles(filepath.Join(opts.DataDir, "logs"), opts.LogsCap)
		freed += n
	}
	return freed, nil
}

type fileEntry struct {
	path string
	size int64
	mod  time.Time
}

// trimOldestFiles removes the oldest regular files in dir until the total size
// is at or below maxBytes. Returns the number of bytes freed.
func trimOldestFiles(dir string, maxBytes int64) (int64, error) {
	var entries []fileEntry
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		entries = append(entries, fileEntry{path: path, size: info.Size(), mod: info.ModTime()})
		return nil
	})

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].mod.Before(entries[j].mod)
	})

	var total int64
	for _, e := range entries {
		total += e.size
	}

	var freed int64
	for _, e := range entries {
		if total <= maxBytes {
			break
		}
		if err := os.Remove(e.path); err == nil {
			total -= e.size
			freed += e.size
		}
	}
	return freed, nil
}
