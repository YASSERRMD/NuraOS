// Package atomicfile provides power-loss-safe file writes.
//
// On POSIX, a rename(2) is atomic with respect to crashes: after power
// loss either the old file or the new file exists; never a partial write.
// Combined with fsync on the temp file and the parent directory, this
// guarantees durability on ext4 with ordered data journaling.
package atomicfile

import (
	"io/fs"
	"os"
	"path/filepath"
)

// Write atomically replaces the file at path with data and perm.
//
// It implements the write-then-fsync-then-rename pattern:
//  1. Create a uniquely named temp file in the same directory as path.
//  2. Write data and call Sync() (flushes to the block device).
//  3. Set file permissions.
//  4. Rename the temp file to path (atomic on POSIX).
//  5. Sync the parent directory entry (durable directory update).
//
// The parent directory must already exist; Write does not create it.
// On any error before the rename the temp file is removed and path is
// left unchanged.
func Write(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".atomicfile-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	ok := false
	defer func() {
		if !ok {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	ok = true

	// Best-effort: sync the directory so the rename is durable.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
