// Package history implements the NuraOS version history store.
//
// Every successful update (full image or delta) records a HistoryEntry at
// /data/update/history.json. Entries capture the active slot, image SHA-256,
// installed packages, and an optional known-good marker.
//
// Retention policy: at most MaxEntries entries are kept. When the limit is
// reached, the oldest entries that are NOT marked known-good are pruned first.
// Known-good entries are retained until all non-known-good entries are gone.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// DefaultMaxEntries is the default number of history entries retained.
	DefaultMaxEntries = 10
	// historyFile is the path relative to the data directory.
	historyFile = "update/history.json"
)

// Entry is one record in the version history.
type Entry struct {
	// ID is a unique identifier for this history entry.
	ID string `json:"id"`
	// Timestamp is when this entry was recorded (RFC 3339).
	Timestamp string `json:"timestamp"`
	// Slot is the A/B slot that carries this version ("a" or "b").
	Slot string `json:"slot"`
	// ImageSHA is the SHA-256 of the rootfs image.
	ImageSHA string `json:"image_sha,omitempty"`
	// ImageVersion is an optional human-readable version string.
	ImageVersion string `json:"image_version,omitempty"`
	// Source records where the image came from.
	Source string `json:"source,omitempty"`
	// Packages lists the package names and versions installed at this point.
	Packages []string `json:"packages,omitempty"`
	// KnownGood is set when the system is confirmed healthy after this update.
	// Known-good entries are exempt from automatic pruning.
	KnownGood bool `json:"known_good"`
	// TxID links this entry to the update transaction that produced it.
	TxID string `json:"tx_id,omitempty"`
}

// Store manages the version history database.
type Store struct {
	path string
}

// db is the on-disk format.
type db struct {
	Schema  int     `json:"schema"`
	Entries []Entry `json:"entries"`
}

// NewStore returns a Store backed by dataDir/update/history.json.
func NewStore(dataDir string) *Store {
	return &Store{path: filepath.Join(dataDir, historyFile)}
}

// Add appends a new entry and prunes to maxEntries if needed.
// maxEntries <= 0 uses DefaultMaxEntries.
func (s *Store) Add(e Entry, maxEntries int) error {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	if e.ID == "" {
		e.ID = fmt.Sprintf("%x", time.Now().UnixNano())
	}
	if e.Timestamp == "" {
		e.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	d, err := s.load()
	if err != nil {
		return err
	}
	d.Entries = append(d.Entries, e)
	prune(d, maxEntries)
	return s.save(d)
}

// MarkKnownGood sets KnownGood=true on the entry with the given ID.
// Returns an error if the ID is not found.
func (s *Store) MarkKnownGood(id string) error {
	d, err := s.load()
	if err != nil {
		return err
	}
	for i, e := range d.Entries {
		if e.ID == id {
			d.Entries[i].KnownGood = true
			return s.save(d)
		}
	}
	return fmt.Errorf("history entry %q not found", id)
}

// Get returns the entry with the given ID.
func (s *Store) Get(id string) (Entry, bool) {
	d, err := s.load()
	if err != nil {
		return Entry{}, false
	}
	for _, e := range d.Entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// List returns all entries sorted newest-first.
func (s *Store) List() ([]Entry, error) {
	d, err := s.load()
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, len(d.Entries))
	copy(entries, d.Entries)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp > entries[j].Timestamp
	})
	return entries, nil
}

// LatestKnownGood returns the most recent entry marked known-good, or (Entry{}, false).
func (s *Store) LatestKnownGood() (Entry, bool) {
	d, err := s.load()
	if err != nil {
		return Entry{}, false
	}
	var best Entry
	found := false
	for _, e := range d.Entries {
		if e.KnownGood && (!found || e.Timestamp > best.Timestamp) {
			best = e
			found = true
		}
	}
	return best, found
}

// Prune retains at most maxEntries entries; oldest non-known-good entries are
// removed first.
func (s *Store) Prune(maxEntries int) error {
	d, err := s.load()
	if err != nil {
		return err
	}
	prune(d, maxEntries)
	return s.save(d)
}

// RollbackTo switches the active slot to the slot recorded in the history entry
// with the given ID, then appends a rollback record. dataDir is the NuraOS data
// root (e.g. /data).
func (s *Store) RollbackTo(id, dataDir string) error {
	e, ok := s.Get(id)
	if !ok {
		return fmt.Errorf("history entry %q not found", id)
	}
	if err := writeActiveSlot(dataDir, e.Slot); err != nil {
		return fmt.Errorf("write active slot: %w", err)
	}
	rollback := Entry{
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		Slot:         e.Slot,
		ImageSHA:     e.ImageSHA,
		ImageVersion: e.ImageVersion,
		Source:       fmt.Sprintf("rollback to %s", id),
		TxID:         id,
	}
	return s.Add(rollback, DefaultMaxEntries)
}

// writeActiveSlot atomically writes /data/etc/active-slot.
func writeActiveSlot(dataDir, slot string) error {
	slotFile := filepath.Join(dataDir, "etc", "active-slot")
	if err := os.MkdirAll(filepath.Dir(slotFile), 0o755); err != nil {
		return err
	}
	tmp := slotFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(slot+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, slotFile)
}

// --- internal ---

func (s *Store) load() (*db, error) {
	d := &db{Schema: 1}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return d, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}
	if err := json.Unmarshal(data, d); err != nil {
		return nil, fmt.Errorf("parse history: %w", err)
	}
	return d, nil
}

func (s *Store) save(d *db) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// prune removes the oldest non-known-good entries until len(d.Entries) <= max.
// If all non-known-good entries are removed and the count still exceeds max,
// known-good entries are removed oldest-first.
func prune(d *db, max int) {
	if len(d.Entries) <= max {
		return
	}
	// Sort by timestamp ascending (oldest first) for removal.
	sort.SliceStable(d.Entries, func(i, j int) bool {
		return d.Entries[i].Timestamp < d.Entries[j].Timestamp
	})
	// First pass: remove oldest non-known-good.
	filtered := d.Entries[:0]
	removed := 0
	excess := len(d.Entries) - max
	for _, e := range d.Entries {
		if !e.KnownGood && removed < excess {
			removed++
			continue
		}
		filtered = append(filtered, e)
	}
	d.Entries = filtered

	// Second pass: if still over, remove oldest known-good entries.
	if len(d.Entries) > max {
		d.Entries = d.Entries[len(d.Entries)-max:]
	}
}
