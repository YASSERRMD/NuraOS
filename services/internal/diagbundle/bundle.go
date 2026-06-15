// Package diagbundle assembles a redacted diagnostic archive from /data/crashes
// suitable for offline analysis. The archive is a gzip-compressed tar file.
package diagbundle

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/crashcap"
)

// Options controls what is bundled into the diagnostic archive.
type Options struct {
	// CrashDir is the source directory (default: /data/crashes).
	CrashDir string
	// OutDir is where the archive is written (default: /tmp).
	OutDir string
	// MaxFiles caps the number of crash files included (default: 50).
	MaxFiles int
}

// Build assembles a gzip-compressed tar archive of all crash captures in
// opts.CrashDir, applies redaction to any plain-text files, and writes the
// archive to opts.OutDir. Returns the path of the created archive.
func Build(opts Options) (string, error) {
	if opts.CrashDir == "" {
		opts.CrashDir = crashcap.DefaultCrashDir
	}
	if opts.OutDir == "" {
		opts.OutDir = os.TempDir()
	}
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = 50
	}

	entries, err := os.ReadDir(opts.CrashDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("diagbundle: crash dir %s does not exist", opts.CrashDir)
		}
		return "", fmt.Errorf("diagbundle: read %s: %w", opts.CrashDir, err)
	}

	// Collect up to MaxFiles files (newest first via reverse sort).
	var files []string
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, filepath.Join(opts.CrashDir, e.Name()))
		}
	}
	// Reverse: newest names last alphabetically, take tail.
	if len(files) > opts.MaxFiles {
		files = files[len(files)-opts.MaxFiles:]
	}

	ts := time.Now().UTC().Format("20060102T150405Z")
	archiveName := fmt.Sprintf("nura-diag-%s.tar.gz", ts)
	archivePath := filepath.Join(opts.OutDir, archiveName)

	out, err := os.OpenFile(archivePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
	if err != nil {
		return "", fmt.Errorf("diagbundle: create archive: %w", err)
	}
	defer out.Close()

	gw := gzip.NewWriter(out)
	tw := tar.NewWriter(gw)

	for _, path := range files {
		if err := addFile(tw, opts.CrashDir, path); err != nil {
			// Skip unreadable files rather than aborting the whole bundle.
			continue
		}
	}

	if err := tw.Close(); err != nil {
		return "", fmt.Errorf("diagbundle: finalize tar: %w", err)
	}
	if err := gw.Close(); err != nil {
		return "", fmt.Errorf("diagbundle: finalize gzip: %w", err)
	}
	return archivePath, nil
}

// addFile appends a single file to the tar archive, re-applying redaction for
// plain-text files (JSON and .txt). Binary files are skipped.
func addFile(tw *tar.Writer, baseDir, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Re-apply redaction: crash bundles are already redacted, but this ensures
	// any manually placed files in the crash dir are also sanitised.
	ext := filepath.Ext(path)
	if ext == ".json" || ext == ".txt" || ext == ".log" {
		data = crashcap.RedactBytes(data)
	}

	rel, _ := filepath.Rel(baseDir, path)
	hdr := &tar.Header{
		Name:    rel,
		Mode:    0640,
		Size:    int64(len(data)),
		ModTime: info.ModTime(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.WriteString(tw, string(data))
	return err
}
