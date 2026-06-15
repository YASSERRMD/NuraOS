//go:build !linux

package cgroup

import (
	"context"
	"log/slog"

	"github.com/yasserrmd/nuraos/services/internal/journal"
	"github.com/yasserrmd/nuraos/services/internal/unit"
)

// EnableControllers is a no-op on non-Linux.
func (m *Manager) EnableControllers() error { return nil }

// Create is a no-op on non-Linux.
func (m *Manager) Create(_ string, _ *unit.Resources) error { return nil }

// AddPid is a no-op on non-Linux.
func (m *Manager) AddPid(_ string, _ int) error { return nil }

// Delete is a no-op on non-Linux.
func (m *Manager) Delete(_ string) error { return nil }

// WatchOOM is a no-op on non-Linux.
func (m *Manager) WatchOOM(_ context.Context, _ string, _ *slog.Logger, _ *journal.Writer) {}

// ReadStats returns nil on non-Linux.
func (m *Manager) ReadStats(_ string) *Stats { return nil }

// SliceStats returns an empty map on non-Linux.
func (m *Manager) SliceStats(services []string) map[string]*Stats {
	out := make(map[string]*Stats, len(services))
	for _, s := range services {
		out[s] = nil
	}
	return out
}
