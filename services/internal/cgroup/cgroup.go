// Package cgroup provides helpers for creating and reading cgroup v2 entries.
//
// On non-Linux platforms all management functions are no-ops and all read
// functions return nil, so the package compiles everywhere.
package cgroup

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// DefaultRoot is the unified cgroup v2 mount point.
	DefaultRoot = "/sys/fs/cgroup"
	// DefaultSlice is the parent slice under which service cgroups are created.
	DefaultSlice = "nura.slice"
)

// Stats holds a point-in-time snapshot of a cgroup's resource consumption.
type Stats struct {
	// CPUUsageUsec is total CPU time consumed in microseconds.
	CPUUsageUsec uint64
	// MemoryCurrent is current memory usage in bytes.
	MemoryCurrent uint64
	// MemoryMax is the configured hard limit in bytes, or 0 if unlimited.
	MemoryMax uint64
	// OOMKills is the total number of OOM-kill events since the cgroup was created.
	OOMKills uint64
}

// Manager creates and manages service cgroups under a slice.
type Manager struct {
	Root  string // mount point; defaults to DefaultRoot
	Slice string // parent slice name; defaults to DefaultSlice
}

// NewManager returns a Manager with sensible defaults.
func NewManager() *Manager {
	return &Manager{Root: DefaultRoot, Slice: DefaultSlice}
}

// parseMemory converts a human-readable memory string to the cgroup v2 format.
// "0" or "" maps to "max" (unlimited). Other values are converted to byte counts.
func parseMemory(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" || strings.EqualFold(s, "max") {
		return "max", nil
	}

	lower := strings.ToLower(s)
	var multiplier uint64 = 1
	switch {
	case strings.HasSuffix(lower, "g"):
		multiplier = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	case strings.HasSuffix(lower, "m"):
		multiplier = 1024 * 1024
		s = s[:len(s)-1]
	case strings.HasSuffix(lower, "k"):
		multiplier = 1024
		s = s[:len(s)-1]
	}

	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid memory value %q: %w", s, err)
	}
	return strconv.FormatUint(n*multiplier, 10), nil
}

// clampCPUWeight returns w clamped to the cgroup v2 cpu.weight range [1, 10000].
func clampCPUWeight(w int) int {
	if w <= 0 {
		return 100
	}
	if w > 10000 {
		return 10000
	}
	return w
}
