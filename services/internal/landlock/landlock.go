// Package landlock applies Linux Landlock filesystem confinement rules.
//
// Landlock (introduced in Linux 5.13) allows a process to restrict its own
// filesystem access to a declared set of paths and rights. The restriction is
// inherited across fork and exec, so it applies to the entire service lifetime
// including the su/sh privilege-drop chain.
//
// On non-Linux platforms all functions are no-ops.
package landlock

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Access is a Landlock filesystem access right name (human-readable).
type Access string

const (
	AccessExecute   Access = "execute"
	AccessWriteFile Access = "write_file"
	AccessReadFile  Access = "read_file"
	AccessReadDir   Access = "read_dir"
	AccessRemoveDir Access = "remove_dir"
	AccessRemoveFile Access = "remove_file"
	AccessMakeChar  Access = "make_char"
	AccessMakeDir   Access = "make_dir"
	AccessMakeReg   Access = "make_reg"
	AccessMakeSock  Access = "make_sock"
	AccessMakeFifo  Access = "make_fifo"
	AccessMakeBlock Access = "make_block"
	AccessMakeSym   Access = "make_sym"
)

// PathRule allows a set of access rights for a path and everything beneath it.
type PathRule struct {
	Path   string   `toml:"path"`
	Access []Access `toml:"access"`
}

// Profile is the in-memory representation of a per-service Landlock profile.
type Profile struct {
	Paths []PathRule `toml:"paths"`
}

// Load reads and parses a TOML Landlock profile file.
func Load(path string) (*Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("landlock profile: %w", err)
	}
	defer f.Close()

	var p Profile
	if _, err := toml.NewDecoder(f).Decode(&p); err != nil {
		return nil, fmt.Errorf("landlock profile %s: %w", path, err)
	}
	return &p, nil
}
