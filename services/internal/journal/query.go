package journal

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Filter constrains which records a query returns.
type Filter struct {
	// Service filters by exact service name. Empty means all.
	Service string
	// MinPriority sets a lower bound (inclusive): only records with Pri <= MinPriority
	// pass (lower number = higher severity). Zero means PriEmergency; use PriDebug for all.
	MinPriority Priority
	// Since excludes records with Time before this value. Zero means no lower bound.
	Since time.Time
	// Until excludes records with Time at or after this value. Zero means no upper bound.
	Until time.Time
	// Limit caps the number of records returned. 0 means unlimited.
	Limit int
}

// Query reads records from the journal directory that match f.
// It reads day files in chronological order.
func Query(dir string, f Filter) ([]Record, error) {
	files, err := listJournalFiles(dir)
	if err != nil {
		return nil, err
	}
	if f.MinPriority == 0 && f.Since.IsZero() {
		f.MinPriority = PriDebug // default: include everything
	}

	var results []Record
	for _, jf := range files {
		recs, err := scanFile(filepath.Join(dir, jf.name), f)
		if err != nil {
			continue
		}
		results = append(results, recs...)
		if f.Limit > 0 && len(results) >= f.Limit {
			results = results[:f.Limit]
			break
		}
	}
	return results, nil
}

// Tail returns the last n records from the journal matching f.
func Tail(dir string, n int, f Filter) ([]Record, error) {
	f.Limit = 0 // scan all
	all, err := Query(dir, f)
	if err != nil {
		return nil, err
	}
	if n > 0 && len(all) > n {
		all = all[len(all)-n:]
	}
	return all, nil
}

func scanFile(path string, f Filter) ([]Record, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var results []Record
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		r, err := UnmarshalNDJSON(line)
		if err != nil {
			continue // skip malformed lines
		}
		if matches(r, f) {
			results = append(results, r)
			if f.Limit > 0 && len(results) >= f.Limit {
				break
			}
		}
	}
	return results, scanner.Err()
}

func matches(r Record, f Filter) bool {
	if f.Service != "" && r.Service != f.Service {
		return false
	}
	if r.Pri > f.MinPriority {
		return false
	}
	if !f.Since.IsZero() && r.Time.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && !r.Time.Before(f.Until) {
		return false
	}
	return true
}

// Follow streams new records from the latest journal file as they are written.
// It calls onRecord for each new record matching f until ctx is cancelled or
// stopCh is closed. The initial scan starts from the current end of the file
// (tail mode: no backfill of old records).
func Follow(dir string, f Filter, stopCh <-chan struct{}, onRecord func(Record)) {
	pollInterval := 200 * time.Millisecond

	var curPath string
	var curOffset int64

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		// Find the latest journal file.
		files, _ := listJournalFiles(dir)
		if len(files) == 0 {
			sleep(pollInterval, stopCh)
			continue
		}
		latest := filepath.Join(dir, files[len(files)-1].name)

		// If the file changed (new day), reset offset.
		if latest != curPath {
			curPath = latest
			// Start from end-of-current-file to avoid backfilling.
			info, err := os.Stat(curPath)
			if err == nil {
				curOffset = info.Size()
			} else {
				curOffset = 0
			}
		}

		file, err := os.Open(curPath)
		if err != nil {
			sleep(pollInterval, stopCh)
			continue
		}

		_, _ = file.Seek(curOffset, 0)
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			r, err := UnmarshalNDJSON(line)
			if err != nil {
				continue
			}
			if matches(r, f) {
				onRecord(r)
			}
		}
		pos, _ := file.Seek(0, 1)
		curOffset = pos
		file.Close()

		sleep(pollInterval, stopCh)
	}
}

func sleep(d time.Duration, stopCh <-chan struct{}) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-stopCh:
	}
}

// ServiceNames returns all unique service names recorded in the journal.
func ServiceNames(dir string) ([]string, error) {
	files, err := listJournalFiles(dir)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	for _, jf := range files {
		path := filepath.Join(dir, jf.name)
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			r, err := UnmarshalNDJSON(scanner.Bytes())
			if err == nil && r.Service != "" {
				seen[r.Service] = true
			}
		}
		f.Close()
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	_ = strings.Join(names, "") // ensure strings is used
	return names, nil
}
