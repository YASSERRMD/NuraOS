// Package seccomp loads allowlist profiles and applies BPF syscall filters.
//
// On Linux the filter is installed via prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER).
// On other platforms the Apply call is a no-op so the manager compiles everywhere.
package seccomp

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Mode controls what happens when a syscall is not in the allowlist.
type Mode string

const (
	// ModeEnforce denies the syscall and returns EPERM to the caller.
	ModeEnforce Mode = "enforce"
	// ModeLog allows the syscall but records it via the kernel audit log.
	// Use during profile development to discover missing entries.
	ModeLog Mode = "log"
)

// Profile is the in-memory representation of a seccomp profile file.
type Profile struct {
	// Mode overrides the unit-level mode when non-empty.
	Mode Mode `toml:"mode"`
	// Syscalls is the allowlist; all other syscalls use the default action.
	Syscalls []string `toml:"syscalls"`
}

// Load reads and parses a TOML profile file.
func Load(path string) (*Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("seccomp profile: %w", err)
	}
	defer f.Close()

	var p Profile
	if _, err := toml.NewDecoder(f).Decode(&p); err != nil {
		return nil, fmt.Errorf("seccomp profile %s: %w", path, err)
	}
	return &p, nil
}
