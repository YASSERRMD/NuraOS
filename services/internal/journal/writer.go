package journal

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultMaxSize is the total size cap across all journal files (100 MiB).
	DefaultMaxSize = 100 * 1024 * 1024
	// filePattern is the day-file naming pattern.
	filePattern = "2006-01-02.journal"
)

// Writer appends records to day-partitioned NDJSON files under dir.
// When total journal size exceeds MaxSize, it deletes the oldest day files.
type Writer struct {
	dir     string
	maxSize int64
	mu      sync.Mutex
	file    *os.File
	today   string
}

// NewWriter opens a Writer rooted at dir with the given total size cap.
// dir is created if absent.
func NewWriter(dir string, maxSize int64) (*Writer, error) {
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("journal dir: %w", err)
	}
	return &Writer{dir: dir, maxSize: maxSize}, nil
}

// Write appends r to the current day file.
func (w *Writer) Write(r Record) error {
	line, err := r.MarshalNDJSON()
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.ensureFile(r.Time); err != nil {
		return err
	}

	if _, err := w.file.Write(line); err != nil {
		return err
	}

	go w.enforceCap()
	return nil
}

// Close flushes and closes the current file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}

// Dir returns the journal directory.
func (w *Writer) Dir() string { return w.dir }

// ensureFile opens or rotates to the correct day file. Must be called with w.mu held.
func (w *Writer) ensureFile(t time.Time) error {
	day := t.UTC().Format(filePattern)
	if w.today == day && w.file != nil {
		return nil
	}
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	path := filepath.Join(w.dir, day)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open journal file %s: %w", path, err)
	}
	w.file = f
	w.today = day
	return nil
}

// enforceCap removes the oldest day files until total usage is below maxSize.
func (w *Writer) enforceCap() {
	files, err := listJournalFiles(w.dir)
	if err != nil || len(files) == 0 {
		return
	}

	var total int64
	for _, f := range files {
		total += f.size
	}
	if total <= w.maxSize {
		return
	}

	// Sort oldest first; delete until under cap.
	for _, f := range files {
		if total <= w.maxSize {
			break
		}
		path := filepath.Join(w.dir, f.name)
		if err := os.Remove(path); err == nil {
			total -= f.size
		}
	}
}

type journalFile struct {
	name string
	size int64
}

func listJournalFiles(dir string) ([]journalFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []journalFile
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".journal") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Mode()&fs.ModeType != 0 {
			continue
		}
		files = append(files, journalFile{name: e.Name(), size: info.Size()})
	}
	// Sort by name (which is the date, lexicographic = chronological).
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, nil
}
