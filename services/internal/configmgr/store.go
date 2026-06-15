package configmgr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// DefaultConfigDir is the canonical config directory on the appliance.
	DefaultConfigDir = "/data/config"
	// configFile is the live config filename within ConfigDir.
	configFile = "nura.json"
	// historyFile is the append-only version history log.
	historyFile = "history.jsonl"
	// MaxHistory is the maximum number of history entries retained.
	MaxHistory = 50
)

// HistoryEntry records a single config version in the history log.
type HistoryEntry struct {
	// Version is the config version number.
	Version int `json:"version"`
	// AppliedAt is the UTC timestamp when this config became active.
	AppliedAt time.Time `json:"applied_at"`
	// Snapshot is the full config at this version.
	Snapshot Config `json:"snapshot"`
}

// Store manages persistent config on disk with atomic apply and history.
type Store struct {
	dir string
	mu  sync.RWMutex
}

// NewStore returns a Store rooted at dir.
func NewStore(dir string) *Store {
	if dir == "" {
		dir = DefaultConfigDir
	}
	return &Store{dir: dir}
}

// Load reads and validates the current config from disk. Returns
// DefaultConfig() if no config file exists yet.
func (s *Store) Load() (Config, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.dir, configFile)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("configmgr: read %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("configmgr: parse %s: %w", path, err)
	}
	return cfg, nil
}

// Apply validates next, increments its version, atomically writes it to disk,
// and appends a history entry. Returns an error (without writing anything) if
// validation fails.
func (s *Store) Apply(next Config) error {
	if err := next.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0750); err != nil {
		return fmt.Errorf("configmgr: mkdir %s: %w", s.dir, err)
	}

	// Read current version to set next version correctly.
	current, _ := s.loadLocked()
	next.Version = current.Version + 1

	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("configmgr: marshal: %w", err)
	}

	// Atomic write: write to temp file then rename.
	tmp := filepath.Join(s.dir, ".nura.json.tmp")
	if err := os.WriteFile(tmp, data, 0640); err != nil {
		return fmt.Errorf("configmgr: write tmp: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(s.dir, configFile)); err != nil {
		return fmt.Errorf("configmgr: rename: %w", err)
	}

	return s.appendHistory(next)
}

// History returns all config history entries, oldest first.
func (s *Store) History() ([]HistoryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readHistory()
}

// RollbackTo reloads and applies the config at the given version.
// Returns an error if the version is not found in history.
func (s *Store) RollbackTo(version int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.readHistory()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Version == version {
			target := e.Snapshot
			// Validate before applying.
			if err := target.Validate(); err != nil {
				return fmt.Errorf("configmgr: rollback target v%d is invalid: %w", version, err)
			}
			// Apply without re-locking (already held).
			current, _ := s.loadLocked()
			target.Version = current.Version + 1
			data, _ := json.MarshalIndent(target, "", "  ")
			tmp := filepath.Join(s.dir, ".nura.json.tmp")
			if err := os.WriteFile(tmp, data, 0640); err != nil {
				return err
			}
			if err := os.Rename(tmp, filepath.Join(s.dir, configFile)); err != nil {
				return err
			}
			return s.appendHistory(target)
		}
	}
	return fmt.Errorf("configmgr: version %d not found in history", version)
}

// Diff loads the current snapshot from disk, compares it to running, and
// returns a DriftReport.
func (s *Store) Diff(running Config) (DriftReport, error) {
	snapshot, err := s.Load()
	if err != nil {
		return DriftReport{}, err
	}
	return DetectDrift(snapshot, running), nil
}

// --- internal helpers ---

func (s *Store) loadLocked() (Config, error) {
	path := filepath.Join(s.dir, configFile)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	return cfg, json.Unmarshal(data, &cfg)
}

func (s *Store) readHistory() ([]HistoryEntry, error) {
	path := filepath.Join(s.dir, historyFile)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("configmgr: read history: %w", err)
	}

	var entries []HistoryEntry
	dec := json.NewDecoder(
		// Wrap data in a reader.
		jsonLinesReader(data),
	)
	for dec.More() {
		var e HistoryEntry
		if err := dec.Decode(&e); err != nil {
			break
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (s *Store) appendHistory(cfg Config) error {
	e := HistoryEntry{
		Version:   cfg.Version,
		AppliedAt: time.Now().UTC(),
		Snapshot:  cfg,
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, historyFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return fmt.Errorf("configmgr: open history: %w", err)
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}
