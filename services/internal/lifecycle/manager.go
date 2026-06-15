package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/unit"
)

// StopTimeout is the per-service grace period between SIGTERM and SIGKILL.
const StopTimeout = 15 * time.Second

// Manager orchestrates service units according to a resolved start plan.
type Manager struct {
	log      *slog.Logger
	mu       sync.Mutex
	statuses map[string]*serviceStatus
	procs    map[string]*serviceRun
}

// NewManager creates a Manager with the given logger.
func NewManager(log *slog.Logger) *Manager {
	return &Manager{
		log:      log,
		statuses: make(map[string]*serviceStatus),
		procs:    make(map[string]*serviceRun),
	}
}

// Status returns a snapshot of the named service's status.
func (m *Manager) Status(name string) (StatusSnapshot, bool) {
	m.mu.Lock()
	s, ok := m.statuses[name]
	m.mu.Unlock()
	if !ok {
		return StatusSnapshot{}, false
	}
	return s.snapshot(), true
}

// AllStatuses returns snapshots of all service statuses.
func (m *Manager) AllStatuses() []StatusSnapshot {
	m.mu.Lock()
	keys := make([]*serviceStatus, 0, len(m.statuses))
	for _, s := range m.statuses {
		keys = append(keys, s)
	}
	m.mu.Unlock()

	out := make([]StatusSnapshot, len(keys))
	for i, s := range keys {
		out[i] = s.snapshot()
	}
	return out
}

// StartPlan starts all units in order, gating on readiness for required deps.
func (m *Manager) StartPlan(ctx context.Context, plan []*unit.Unit) {
	for _, u := range plan {
		m.mu.Lock()
		if _, exists := m.statuses[u.Name]; !exists {
			m.statuses[u.Name] = newServiceStatus(u.Name)
		}
		m.mu.Unlock()

		m.log.Info("starting unit", "name", u.Name)
		run := m.launchUnit(ctx, u)
		m.mu.Lock()
		m.procs[u.Name] = run
		m.mu.Unlock()

		// Gate: wait for this unit to be ready before starting dependants.
		if len(u.Requires) > 0 {
			if !m.waitReady(ctx, u) {
				m.log.Warn("readiness gating timed out; continuing", "name", u.Name)
			}
		}
	}
}

// ShutdownPlan stops all units in reverse of the given plan order.
func (m *Manager) ShutdownPlan(plan []*unit.Unit) {
	for i := len(plan) - 1; i >= 0; i-- {
		u := plan[i]
		m.mu.Lock()
		run := m.procs[u.Name]
		status := m.statuses[u.Name]
		m.mu.Unlock()
		if run == nil {
			continue
		}
		if status != nil {
			_ = status.transition(StateStopping, "ordered shutdown")
		}
		m.log.Info("stopping unit", "name", u.Name)
		run.stop()
		if status != nil {
			_ = status.transition(StateInactive, "stopped")
		}
	}
}

// serviceRun holds the running context for a single service instance.
type serviceRun struct {
	u      *unit.Unit
	cmd    *exec.Cmd
	cancel context.CancelFunc
	mu     sync.Mutex
}

func (r *serviceRun) stop() {
	r.mu.Lock()
	cmd := r.cmd
	r.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(StopTimeout):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	r.cancel()
}

// launchUnit starts u and its restart supervisor goroutine.
func (m *Manager) launchUnit(ctx context.Context, u *unit.Unit) *serviceRun {
	runCtx, cancel := context.WithCancel(ctx)
	run := &serviceRun{u: u, cancel: cancel}
	go m.restartLoop(runCtx, u, run)
	return run
}

// restartLoop manages one service's process lifecycle.
func (m *Manager) restartLoop(ctx context.Context, u *unit.Unit, run *serviceRun) {
	m.mu.Lock()
	status := m.statuses[u.Name]
	m.mu.Unlock()

	policy := u.Restart.Policy
	backoff := time.Duration(u.Restart.BackoffInit) * time.Second
	backoffMax := time.Duration(u.Restart.BackoffMax) * time.Second
	crashWindow := time.Duration(u.Restart.CrashLoopWindow) * time.Second
	crashBackoff := time.Duration(u.Restart.CrashLoopBackoff) * time.Second
	crashTimes := make([]time.Time, 0, u.Restart.CrashLoopLimit+1)

	for {
		if ctx.Err() != nil {
			return
		}

		_ = status.transition(StateStarting, "starting process")

		cmd, readyCh, err := m.spawnProcess(ctx, u)
		if err != nil {
			m.log.Error("spawn failed", "name", u.Name, "err", err)
			_ = status.transition(StateFailed, "spawn error: "+err.Error())
			if policy == unit.RestartNo {
				return
			}
		} else {
			run.mu.Lock()
			run.cmd = cmd
			run.mu.Unlock()
			status.mu.Lock()
			status.pid = cmd.Process.Pid
			status.mu.Unlock()
			m.log.Info("unit launched", "name", u.Name, "pid", cmd.Process.Pid)

			// Wait for readiness signal (notify type).
			if readyCh != nil {
				select {
				case <-readyCh:
					_ = status.transition(StateReady, "notify: READY=1")
					m.log.Info("unit ready (notify)", "name", u.Name)
				case <-ctx.Done():
					return
				}
			} else {
				_ = status.transition(StateReady, "launched")
			}
			_ = status.transition(StateRunning, "ready -> running")

			exitErr := cmd.Wait()
			if ctx.Err() != nil {
				return
			}

			exitCode := 0
			if exitErr != nil {
				if ee, ok := exitErr.(*exec.ExitError); ok {
					exitCode = ee.ExitCode()
				}
			}
			m.log.Info("unit exited", "name", u.Name, "code", exitCode)
			status.mu.Lock()
			status.lastExit = exitCode
			status.mu.Unlock()

			shouldRestart := false
			switch policy {
			case unit.RestartAlways:
				shouldRestart = true
			case unit.RestartOnFailure:
				shouldRestart = exitCode != 0
			case unit.RestartNo:
				shouldRestart = false
			}

			if !shouldRestart {
				_ = status.transition(StateFailed, fmt.Sprintf("exit %d; policy=%s", exitCode, policy))
				return
			}

			// Crash-loop detection.
			now := time.Now()
			crashTimes = append(crashTimes, now)
			cutoff := now.Add(-crashWindow)
			trim := 0
			for trim < len(crashTimes) && crashTimes[trim].Before(cutoff) {
				trim++
			}
			crashTimes = crashTimes[trim:]

			if len(crashTimes) >= u.Restart.CrashLoopLimit {
				_ = status.transition(StateFailed, fmt.Sprintf("crash-loop: %d crashes", len(crashTimes)))
				m.log.Warn("crash loop; pausing", "name", u.Name, "pause", crashBackoff)
				select {
				case <-time.After(crashBackoff):
				case <-ctx.Done():
					return
				}
				crashTimes = crashTimes[:0]
				backoff = time.Duration(u.Restart.BackoffInit) * time.Second
			}
		}

		status.mu.Lock()
		status.restarts++
		status.mu.Unlock()
		m.log.Info("restarting unit", "name", u.Name, "backoff", backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		if backoff > 0 {
			backoff *= 2
		} else {
			backoff = time.Duration(u.Restart.BackoffInit) * time.Second
		}
		if backoff > backoffMax {
			backoff = backoffMax
		}
	}
}

// spawnProcess forks the service. For notify-type units it returns a readyCh
// that closes when the child writes "READY=1".
func (m *Manager) spawnProcess(ctx context.Context, u *unit.Unit) (*exec.Cmd, <-chan struct{}, error) {
	var args []string
	if u.User != "" && u.User != "root" {
		args = []string{"su", "-s", "/bin/sh", u.User, "-c", shellJoin(u.Exec, u.Args)}
	} else {
		args = append([]string{u.Exec}, u.Args...)
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var readyCh chan struct{}
	if u.Type == unit.TypeNotify {
		r, w, err := newNotifyPipe()
		if err != nil {
			return nil, nil, fmt.Errorf("notify pipe: %w", err)
		}
		cmd.ExtraFiles = []*os.File{w}
		cmd.Env = append(os.Environ(), "NOTIFY_FD=3")
		readyCh = make(chan struct{}, 1)
		go func() {
			defer r.Close()
			notifyListener(r, readyCh)
		}()
		defer w.Close()
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return cmd, readyCh, nil
}

// waitReady blocks until the unit is ready per its readiness probe config.
func (m *Manager) waitReady(ctx context.Context, u *unit.Unit) bool {
	probe := u.Readiness
	timeout := time.Duration(probe.Timeout) * time.Second
	deadline := time.Now().Add(timeout)

	switch probe.Type {
	case unit.ReadinessHTTP:
		client := &http.Client{Timeout: 2 * time.Second}
		for time.Now().Before(deadline) {
			resp, err := client.Get(probe.URL)
			if err == nil && resp.StatusCode < 500 {
				resp.Body.Close()
				m.log.Info("HTTP ready", "name", u.Name)
				return true
			}
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return false
			}
		}
		return false

	case unit.ReadinessSocket:
		for time.Now().Before(deadline) {
			c, err := net.DialTimeout("unix", probe.Socket, time.Second)
			if err == nil {
				c.Close()
				m.log.Info("socket ready", "name", u.Name)
				return true
			}
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return false
			}
		}
		return false

	default:
		// For notify and none types: poll the status state.
		for time.Now().Before(deadline) {
			m.mu.Lock()
			s := m.statuses[u.Name]
			m.mu.Unlock()
			if s != nil {
				st := s.currentState()
				if st == StateReady || st == StateRunning {
					return true
				}
				if st == StateFailed {
					return false
				}
			}
			select {
			case <-time.After(200 * time.Millisecond):
			case <-ctx.Done():
				return false
			}
		}
		return false
	}
}

func shellJoin(exe string, args []string) string {
	parts := append([]string{exe}, args...)
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = "'" + strings.ReplaceAll(p, "'", "'\\''") + "'"
	}
	return strings.Join(quoted, " ")
}
