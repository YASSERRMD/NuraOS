package pkgmgr

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// InstallRecord is the database entry for a single installed package.
type InstallRecord struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description,omitempty"`
	InstalledAt string   `json:"installed_at"`
	Files       []string `json:"files"`
	Depends     []string `json:"depends,omitempty"`
	RemoveHook  string   `json:"remove_hook,omitempty"`
}

// DB is the in-memory representation of the package database.
type DB struct {
	Schema   int                      `json:"schema"`
	Packages map[string]InstallRecord `json:"packages"`
}

// LoadDB reads the package database from path. If the file does not exist an
// empty database is returned.
func LoadDB(path string) (*DB, error) {
	db := &DB{Schema: SchemaVersion, Packages: make(map[string]InstallRecord)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return db, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read package db: %w", err)
	}
	if err := json.Unmarshal(data, db); err != nil {
		return nil, fmt.Errorf("parse package db: %w", err)
	}
	if db.Packages == nil {
		db.Packages = make(map[string]InstallRecord)
	}
	return db, nil
}

// Save writes the database to path atomically via a temp file and rename.
func (db *DB) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Add records an installed package, overwriting any previous record for the same name.
func (db *DB) Add(rec InstallRecord) {
	db.Packages[rec.Name] = rec
}

// Remove deletes the record for name and returns it. Returns false if not found.
func (db *DB) Remove(name string) (InstallRecord, bool) {
	rec, ok := db.Packages[name]
	if ok {
		delete(db.Packages, name)
	}
	return rec, ok
}

// Get returns the record for name.
func (db *DB) Get(name string) (InstallRecord, bool) {
	rec, ok := db.Packages[name]
	return rec, ok
}

// Dependents returns the names of all installed packages that depend on name.
func (db *DB) Dependents(name string) []string {
	var out []string
	for _, rec := range db.Packages {
		for _, dep := range rec.Depends {
			if dep == name {
				out = append(out, rec.Name)
				break
			}
		}
	}
	return out
}
