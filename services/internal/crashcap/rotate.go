package crashcap

import (
	"os"
	"path/filepath"
	"sort"
)

// rotate removes the oldest JSON files in dir until at most keep files remain.
func rotate(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}

	// Sort ascending by name (timestamps are embedded in the name in ISO format,
	// so lexicographic order equals chronological order).
	sort.Strings(files)

	for len(files) > keep {
		if err := os.Remove(files[0]); err != nil && !os.IsNotExist(err) {
			return err
		}
		files = files[1:]
	}
	return nil
}
