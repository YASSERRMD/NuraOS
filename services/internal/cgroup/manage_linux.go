//go:build linux

package cgroup

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/journal"
	"github.com/yasserrmd/nuraos/services/internal/unit"
)

// cgroupPath returns the absolute path of the named service cgroup.
func (m *Manager) cgroupPath(service string) string {
	return filepath.Join(m.Root, m.Slice, service+".service")
}

// EnableControllers writes "+cpu +memory +io" to the subtree_control file of
// the root cgroup and the slice so children can use those controllers.
// Errors are advisory only; the caller should log and continue.
func (m *Manager) EnableControllers() error {
	const controllers = "+cpu +memory +io"

	rootCtl := filepath.Join(m.Root, "cgroup.subtree_control")
	if err := os.WriteFile(rootCtl, []byte(controllers), 0644); err != nil {
		return fmt.Errorf("root subtree_control: %w", err)
	}

	sliceDir := filepath.Join(m.Root, m.Slice)
	if err := os.MkdirAll(sliceDir, 0755); err != nil {
		return fmt.Errorf("mkdir slice: %w", err)
	}
	sliceCtl := filepath.Join(sliceDir, "cgroup.subtree_control")
	if err := os.WriteFile(sliceCtl, []byte(controllers), 0644); err != nil {
		return fmt.Errorf("slice subtree_control: %w", err)
	}
	return nil
}

// Create creates the service cgroup and writes resource limits from r.
func (m *Manager) Create(service string, r *unit.Resources) error {
	dir := m.cgroupPath(service)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir cgroup %s: %w", dir, err)
	}

	// cpu.weight
	weight := clampCPUWeight(r.CPUWeight)
	if err := writeFile(filepath.Join(dir, "cpu.weight"), strconv.Itoa(weight)); err != nil {
		return fmt.Errorf("cpu.weight: %w", err)
	}

	// memory.max
	memMax, err := parseMemory(r.MemoryMax)
	if err != nil {
		return fmt.Errorf("memory_max: %w", err)
	}
	if err := writeFile(filepath.Join(dir, "memory.max"), memMax); err != nil {
		return fmt.Errorf("memory.max: %w", err)
	}

	// io.weight (only if configured)
	if r.IOWeight > 0 {
		ioW := clampCPUWeight(r.IOWeight)
		if err := writeFile(filepath.Join(dir, "io.weight"), fmt.Sprintf("default %d", ioW)); err != nil {
			return fmt.Errorf("io.weight: %w", err)
		}
	}

	return nil
}

// AddPid moves pid into the service's cgroup by writing to cgroup.procs.
func (m *Manager) AddPid(service string, pid int) error {
	procs := filepath.Join(m.cgroupPath(service), "cgroup.procs")
	return writeFile(procs, strconv.Itoa(pid))
}

// Delete removes the service cgroup directory. It fails if the cgroup still
// has live processes; call after all processes have exited.
func (m *Manager) Delete(service string) error {
	dir := m.cgroupPath(service)
	return os.Remove(dir)
}

// WatchOOM polls memory.events for the service cgroup and logs an OOM event
// to log and, when non-nil, to jw whenever the oom_kill counter increases.
// The goroutine exits when ctx is cancelled.
func (m *Manager) WatchOOM(ctx context.Context, service string, log *slog.Logger, jw *journal.Writer) {
	eventsPath := filepath.Join(m.cgroupPath(service), "memory.events")
	var lastOOM uint64
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := readOOMKill(eventsPath)
			if err != nil {
				// File may not exist if memory controller is inactive.
				continue
			}
			if n > lastOOM {
				delta := n - lastOOM
				lastOOM = n
				log.Error("OOM kill detected in service cgroup",
					"service", service, "oom_kills", n, "new_kills", delta)
				if jw != nil {
					_ = jw.WriteEntry(journal.Entry{
						Service:  service,
						Priority: journal.PriError,
						Message:  fmt.Sprintf("cgroup OOM kill: %d new kill(s) (total %d)", delta, n),
					})
				}
			}
		}
	}
}

// ReadStats returns a Stats snapshot for service. Returns nil if the cgroup
// does not exist or the required files are unreadable.
func (m *Manager) ReadStats(service string) *Stats {
	return readStats(m.cgroupPath(service))
}

// SliceStats returns stats for all listed services. A nil entry means the
// service cgroup could not be read.
func (m *Manager) SliceStats(services []string) map[string]*Stats {
	out := make(map[string]*Stats, len(services))
	for _, svc := range services {
		out[svc] = m.ReadStats(svc)
	}
	return out
}

// --- helpers ---

func writeFile(path, value string) error {
	return os.WriteFile(path, []byte(value+"\n"), 0644)
}

// readStats reads cpu.stat, memory.current, memory.max, and memory.events
// from the given cgroup directory.
func readStats(dir string) *Stats {
	s := &Stats{}

	// cpu.stat: "usage_usec N"
	if data, err := os.ReadFile(filepath.Join(dir, "cpu.stat")); err == nil {
		s.CPUUsageUsec = parseKV(string(data), "usage_usec")
	}

	// memory.current: single integer
	if data, err := os.ReadFile(filepath.Join(dir, "memory.current")); err == nil {
		v, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		s.MemoryCurrent = v
	}

	// memory.max: integer or "max"
	if data, err := os.ReadFile(filepath.Join(dir, "memory.max")); err == nil {
		val := strings.TrimSpace(string(data))
		if val != "max" {
			v, _ := strconv.ParseUint(val, 10, 64)
			s.MemoryMax = v
		}
	}

	// memory.events: "oom_kill N"
	if data, err := os.ReadFile(filepath.Join(dir, "memory.events")); err == nil {
		s.OOMKills = parseKV(string(data), "oom_kill")
	}

	return s
}

// parseKV scans "key N" lines in data and returns the value for key.
func parseKV(data, key string) uint64 {
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[0] == key {
			v, _ := strconv.ParseUint(parts[1], 10, 64)
			return v
		}
	}
	return 0
}

// readOOMKill reads the oom_kill counter from memory.events.
func readOOMKill(eventsPath string) (uint64, error) {
	data, err := os.ReadFile(eventsPath)
	if err != nil {
		return 0, err
	}
	return parseKV(string(data), "oom_kill"), nil
}
