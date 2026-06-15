package unit

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ParseFile reads and parses a single TOML unit file.
func ParseFile(path string) (*Unit, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open unit file %s: %w", path, err)
	}
	defer f.Close()

	var u Unit
	if _, err := toml.NewDecoder(f).Decode(&u); err != nil {
		return nil, fmt.Errorf("parse unit file %s: %w", path, err)
	}

	if u.Name == "" {
		return nil, fmt.Errorf("unit file %s: missing required field 'name'", path)
	}
	if u.Exec == "" {
		return nil, fmt.Errorf("unit file %s: missing required field 'exec'", path)
	}

	u.defaults()
	return &u, nil
}

// LoadDir reads all *.toml files from dir and returns the enabled units.
func LoadDir(dir string) ([]*Unit, error) {
	entries, err := filepath.Glob(filepath.Join(dir, "*.toml"))
	if err != nil {
		return nil, fmt.Errorf("glob unit dir %s: %w", dir, err)
	}

	var units []*Unit
	for _, path := range entries {
		u, err := ParseFile(path)
		if err != nil {
			return nil, err
		}
		if !u.Enabled {
			continue
		}
		units = append(units, u)
	}
	return units, nil
}
