// Package sysmetrics collects OS-level system metrics in a single pass and
// exposes them in a format suitable for Prometheus text exposition.
//
// Metrics collected:
//   - Per-interface network counters (rx/tx bytes, packets, errors, drops)
//   - Kernel entropy pool available bits
//   - Per-service cgroup CPU/memory/OOM statistics
//
// All collection is synchronous and designed to run in under 5 ms on typical
// hardware. A nil Collector is safe to use; all methods return empty/zero values.
package sysmetrics

import (
	"github.com/yasserrmd/nuraos/services/internal/cgroup"
	"github.com/yasserrmd/nuraos/services/internal/entropy"
	"github.com/yasserrmd/nuraos/services/internal/netstat"
)

// Stats holds the OS-level metrics snapshot collected by Collect.
type Stats struct {
	// Interfaces holds per-interface network counters.
	// Empty when /proc/net/dev is not available.
	Interfaces []netstat.IfaceStats

	// EntropyAvailBits is the kernel's estimate of available entropy bits.
	// 0 when /proc/sys/kernel/random/entropy_avail is not readable.
	EntropyAvailBits int

	// CgroupStats maps service name to its cgroup statistics.
	// Nil entries indicate the service cgroup is absent or unreadable.
	CgroupStats map[string]*cgroup.Stats
}

// Collector gathers OS-level metrics. The zero value collects from the
// standard Linux /proc and /sys paths.
type Collector struct {
	// CgroupServices is the list of service names to collect cgroup stats for.
	CgroupServices []string
	cgMgr          *cgroup.Manager
}

// NewCollector returns a Collector that reads cgroup stats for the given
// service names.
func NewCollector(services []string) *Collector {
	return &Collector{
		CgroupServices: services,
		cgMgr:          cgroup.NewManager(),
	}
}

// Collect reads all OS-level metrics and returns a snapshot.
// It is safe to call concurrently; each call opens and closes its own file
// descriptors.
func (c *Collector) Collect() Stats {
	if c == nil {
		return Stats{}
	}

	s := Stats{
		EntropyAvailBits: entropy.AvailBits(),
	}

	// Network counters.
	ifaces, err := netstat.ReadStats()
	if err == nil {
		s.Interfaces = ifaces
	}

	// Per-service cgroup statistics.
	if len(c.CgroupServices) > 0 && c.cgMgr != nil {
		s.CgroupStats = c.cgMgr.SliceStats(c.CgroupServices)
	}

	return s
}
