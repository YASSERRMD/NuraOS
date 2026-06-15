// Package diskmon monitors filesystem space and enforces per-subtree soft quotas.
// It surfaces usage in gateway /status and /metrics, triggers automatic reclaim
// at the warn threshold, and blocks new sessions at the critical threshold.
package diskmon

import (
	"context"
	"log/slog"
	"sync/atomic"
	"syscall"
	"time"
)

// DefaultWarnPct and DefaultCriticalPct are percentage-of-total-space thresholds.
const (
	DefaultWarnPct     = 80.0
	DefaultCriticalPct = 95.0
	DefaultInterval    = 30 * time.Second
)

// Status represents the current disk health level.
type Status int32

const (
	StatusOK       Status = 0
	StatusWarn     Status = 1
	StatusCritical Status = 2
)

func (s Status) String() string {
	switch s {
	case StatusWarn:
		return "warn"
	case StatusCritical:
		return "critical"
	default:
		return "ok"
	}
}

// Usage holds a point-in-time disk usage snapshot.
type Usage struct {
	Total     uint64
	Used      uint64
	Available uint64
	UsedPct   float64
}

// DiskUsage returns filesystem usage stats for the filesystem containing path.
func DiskUsage(path string) (Usage, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return Usage{}, err
	}
	bsize := uint64(stat.Bsize)
	total := stat.Blocks * bsize
	free := stat.Bfree * bsize
	avail := stat.Bavail * bsize
	used := total - free
	pct := 0.0
	if total > 0 {
		pct = float64(used) / float64(total) * 100
	}
	return Usage{Total: total, Used: used, Available: avail, UsedPct: pct}, nil
}

// Monitor polls disk usage periodically and transitions between health states.
// All exported fields must be set before calling Run.
type Monitor struct {
	// Path is the filesystem path to monitor (e.g. "/data").
	Path string
	// WarnPct and CriticalPct are percentage-of-total-space thresholds (0-100).
	WarnPct     float64
	CriticalPct float64
	// Interval between polls; 0 uses DefaultInterval.
	Interval time.Duration
	// Log is optional; state transitions are logged when non-nil.
	Log *slog.Logger
	// OnWarn is called once on transition into warn state.
	OnWarn func(Usage)
	// OnCritical is called once on transition into critical state.
	OnCritical func(Usage)
	// OnRecovered is called once when recovering to ok from warn/critical.
	OnRecovered func(Usage)

	status atomic.Int32  // holds Status value
	last   atomic.Value  // holds *Usage
}

// Run polls disk usage until ctx is cancelled. Call in a goroutine.
func (m *Monitor) Run(ctx context.Context) {
	if m.WarnPct <= 0 {
		m.WarnPct = DefaultWarnPct
	}
	if m.CriticalPct <= 0 {
		m.CriticalPct = DefaultCriticalPct
	}
	if m.Interval <= 0 {
		m.Interval = DefaultInterval
	}

	m.poll()
	t := time.NewTicker(m.Interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			m.poll()
		case <-ctx.Done():
			return
		}
	}
}

func (m *Monitor) poll() {
	u, err := DiskUsage(m.Path)
	if err != nil {
		if m.Log != nil {
			m.Log.Warn("disk usage check failed", "path", m.Path, "err", err)
		}
		return
	}
	m.last.Store(&u)

	prev := Status(m.status.Load())
	var next Status
	switch {
	case u.UsedPct >= m.CriticalPct:
		next = StatusCritical
	case u.UsedPct >= m.WarnPct:
		next = StatusWarn
	default:
		next = StatusOK
	}

	if next == prev {
		return
	}
	m.status.Store(int32(next))

	switch next {
	case StatusCritical:
		if m.Log != nil {
			m.Log.Error("disk critical", "path", m.Path, "used_pct", u.UsedPct)
		}
		if m.OnCritical != nil {
			m.OnCritical(u)
		}
	case StatusWarn:
		if m.Log != nil {
			m.Log.Warn("disk warn", "path", m.Path, "used_pct", u.UsedPct)
		}
		if m.OnWarn != nil {
			m.OnWarn(u)
		}
	case StatusOK:
		if m.Log != nil {
			m.Log.Info("disk recovered", "path", m.Path, "used_pct", u.UsedPct)
		}
		if m.OnRecovered != nil {
			m.OnRecovered(u)
		}
	}
}

// CurrentStatus returns the most recently computed health status.
func (m *Monitor) CurrentStatus() Status {
	return Status(m.status.Load())
}

// LastUsage returns the most recently observed usage, or nil if not yet polled.
func (m *Monitor) LastUsage() *Usage {
	v := m.last.Load()
	if v == nil {
		return nil
	}
	return v.(*Usage)
}

// IsCritical reports whether new sessions should be refused due to disk pressure.
func (m *Monitor) IsCritical() bool {
	return Status(m.status.Load()) == StatusCritical
}
