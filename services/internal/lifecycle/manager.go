package lifecycle

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/cgroup"
	"github.com/yasserrmd/nuraos/services/internal/journal"
	"github.com/yasserrmd/nuraos/services/internal/sockact"
	"github.com/yasserrmd/nuraos/services/internal/unit"
)

// StopTimeout is the per-service grace period between SIGTERM and SIGKILL.
const StopTimeout = 15 * time.Second

// Manager orchestrates service units according to a resolved start plan.
type Manager struct {
	log      *slog.Logger
	journal  *journal.Writer
	cgMgr    *cgroup.Manager
	mu       sync.Mutex
	statuses map[string]*serviceStatus
	procs    map[string]*serviceRun
}

// NewManager creates a Manager with the given logger.
// If journalWriter is non-nil, service stdout/stderr is captured to the journal.
func NewManager(log *slog.Logger, journalWriter *journal.Writer) *Manager {
	cgMgr := cgroup.NewManager()
	if err := cgMgr.EnableControllers(); err != nil {
		log.Warn("cgroup: controller enable failed (resource limits inactive)", "err", err)
	}
	return &Manager{
		log:      log,
		journal:  journalWriter,
		cgMgr:    cgMgr,
		statuses: make(map[string]*serviceStatus),
		procs:    make(map[string]*serviceRun),
	}
}

// CgroupManager returns the cgroup.Manager used by this lifecycle Manager.
// Callers (e.g. the /metrics handler) can use it to read per-service stats.
func (m *Manager) CgroupManager() *cgroup.Manager { return m.cgMgr }

// Journal returns the writer in use, or nil if journaling is disabled.
func (m *Manager) Journal() *journal.Writer { return m.journal }

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

		if u.SocketActivation.Enabled {
			m.log.Info("socket-activating unit", "name", u.Name,
				"network", u.SocketActivation.Network,
				"address", u.SocketActivation.Address)
			go m.socketActivate(ctx, u)
			// Socket-activated units are not in the readiness gate; dependants
			// start immediately after the socket is bound.
			continue
		}

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

// socketActivate manages the lazy-start lifecycle for a socket-activated unit.
// It pre-opens the socket, waits for the first connection, then starts the
// unit and passes the socket fd as LISTEN_FDS=1. When idle_timeout is set it
// also stops the unit after the configured inactivity period.
func (m *Manager) socketActivate(ctx context.Context, u *unit.Unit) {
	sa := u.SocketActivation
	network := sa.Network
	if network == "" {
		network = "tcp"
	}

	holder, err := sockact.NewHolder(network, sa.Address)
	if err != nil {
		m.log.Error("socket activation bind failed", "name", u.Name, "err", err)
		return
	}
	defer holder.Close()
	m.log.Info("socket bound; waiting for first connection",
		"name", u.Name, "address", holder.Address())

	// Idle monitor runs for the lifetime of the socket activation.
	if sa.IdleTimeout > 0 {
		idleTimeout := time.Duration(sa.IdleTimeout) * time.Second
		go sockact.IdleMonitor(ctx, holder, idleTimeout, m.log, func() {
			m.mu.Lock()
			run := m.procs[u.Name]
			status := m.statuses[u.Name]
			m.mu.Unlock()
			if run == nil {
				return
			}
			if status != nil {
				_ = status.transition(StateStopping, "idle timeout")
			}
			m.log.Info("idle-stopping unit", "name", u.Name)
			run.stop()
			m.mu.Lock()
			m.procs[u.Name] = nil
			m.mu.Unlock()
			if status != nil {
				_ = status.transition(StateInactive, "idle-stopped")
			}
		})
	}

	for {
		if ctx.Err() != nil {
			return
		}

		stopCh := make(chan struct{})
		ctxDone := ctx.Done()
		go func() {
			<-ctxDone
			close(stopCh)
		}()

		m.log.Info("waiting for activation connection", "name", u.Name)
		if err := holder.WaitFirstConnection(stopCh); err != nil {
			if ctx.Err() != nil {
				return
			}
			m.log.Warn("activation wait error", "name", u.Name, "err", err)
			return
		}
		holder.TouchActivity()
		m.log.Info("first connection received; activating unit", "name", u.Name)

		// Initialise status entry if absent.
		m.mu.Lock()
		if _, exists := m.statuses[u.Name]; !exists {
			m.statuses[u.Name] = newServiceStatus(u.Name)
		}
		m.mu.Unlock()

		// Spawn the unit, passing the pre-opened socket as LISTEN_FDS.
		run := m.launchSocketUnit(ctx, u, holder)
		m.mu.Lock()
		m.procs[u.Name] = run
		m.mu.Unlock()

		// Wait until the unit stops (idle-stop or crash) before accepting
		// the next activation cycle.
		m.waitRunStopped(ctx, u.Name)
		m.log.Info("socket-activated unit stopped; ready for next activation", "name", u.Name)
	}
}

// launchSocketUnit starts a unit with the pre-opened socket fd passed via
// LISTEN_FDS=1 / LISTEN_PID environment variables.
func (m *Manager) launchSocketUnit(ctx context.Context, u *unit.Unit, holder *sockact.Holder) *serviceRun {
	runCtx, cancel := context.WithCancel(ctx)
	run := &serviceRun{u: u, ctx: runCtx, cancel: cancel}

	go func() {
		m.mu.Lock()
		status := m.statuses[u.Name]
		m.mu.Unlock()

		_ = status.transition(StateStarting, "socket activation")

		f, err := holder.File()
		if err != nil {
			m.log.Error("holder.File() failed", "name", u.Name, "err", err)
			_ = status.transition(StateFailed, "holder.File: "+err.Error())
			cancel()
			return
		}

		var args []string
		if u.User != "" && u.User != "root" {
			args = []string{"su", "-s", "/bin/sh", u.User, "-c", shellJoin(u.Exec, u.Args)}
		} else {
			args = append([]string{u.Exec}, u.Args...)
		}
		args = wrapWithSeccomp(args, u)

		cmd := exec.CommandContext(runCtx, args[0], args[1:]...)
		saAttr := &syscall.SysProcAttr{Setpgid: true}
		applyNamespaces(saAttr, u.Namespaces)
		cmd.SysProcAttr = saAttr

		var saStdout, saStderr io.ReadCloser
		if m.journal != nil {
			saStdout, _ = cmd.StdoutPipe()
			saStderr, _ = cmd.StderrPipe()
		} else {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}

		cmd.ExtraFiles = []*os.File{f.File}
		cmd.Env = append(os.Environ(),
			"LISTEN_FDS=1",
			fmt.Sprintf("LISTEN_PID=%d", os.Getpid()),
		)
		f.Close()

		if err := cmd.Start(); err != nil {
			m.log.Error("socket-activated start failed", "name", u.Name, "err", err)
			_ = status.transition(StateFailed, "start: "+err.Error())
			cancel()
			return
		}

		if m.journal != nil {
			pid := cmd.Process.Pid
			if saStdout != nil {
				go journal.Collect(saStdout, m.journal, u.Name, pid, journal.PriInfo)
			}
			if saStderr != nil {
				go journal.Collect(saStderr, m.journal, u.Name, pid, journal.PriError)
			}
		}

		run.mu.Lock()
		run.cmd = cmd
		run.mu.Unlock()
		status.mu.Lock()
		status.pid = cmd.Process.Pid
		status.mu.Unlock()

		_ = status.transition(StateReady, "socket-activated")
		_ = status.transition(StateRunning, "ready -> running")
		m.log.Info("socket-activated unit running", "name", u.Name, "pid", cmd.Process.Pid)

		_ = cmd.Wait()
		m.log.Info("socket-activated unit exited", "name", u.Name)
		_ = status.transition(StateInactive, "exited")
		cancel()
	}()
	return run
}

// waitRunStopped blocks until the unit's run context is done (stopped/exited).
func (m *Manager) waitRunStopped(ctx context.Context, name string) {
	m.mu.Lock()
	run := m.procs[name]
	m.mu.Unlock()
	if run == nil {
		return
	}
	select {
	case <-run.ctx.Done():
	case <-ctx.Done():
	}
}

// serviceRun holds the running context for a single service instance.
type serviceRun struct {
	u      *unit.Unit
	cmd    *exec.Cmd
	ctx    context.Context
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
	run := &serviceRun{u: u, ctx: runCtx, cancel: cancel}
	go m.restartLoop(runCtx, u, run)
	return run
}

// restartLoop manages one service's process lifecycle.
func (m *Manager) restartLoop(ctx context.Context, u *unit.Unit, run *serviceRun) {
	m.mu.Lock()
	status := m.statuses[u.Name]
	m.mu.Unlock()

	// Create a cgroup for this service and apply resource limits.
	if err := m.cgMgr.Create(u.Name, &u.Resources); err != nil {
		m.log.Warn("cgroup: create failed", "name", u.Name, "err", err)
	} else {
		defer m.cgMgr.Delete(u.Name)
	}

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

			// Place the process in its cgroup and start OOM monitoring.
			if err := m.cgMgr.AddPid(u.Name, cmd.Process.Pid); err != nil {
				m.log.Warn("cgroup: add pid failed", "name", u.Name, "err", err)
			} else {
				go m.cgMgr.WatchOOM(ctx, u.Name, m.log, m.journal)
			}

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
	args = wrapWithSeccomp(args, u)

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	procAttr := &syscall.SysProcAttr{Setpgid: true}
	applyNamespaces(procAttr, u.Namespaces)
	cmd.SysProcAttr = procAttr

	var stdoutPipe, stderrPipe io.ReadCloser
	if m.journal != nil {
		var err error
		stdoutPipe, err = cmd.StdoutPipe()
		if err != nil {
			return nil, nil, fmt.Errorf("stdout pipe: %w", err)
		}
		stderrPipe, err = cmd.StderrPipe()
		if err != nil {
			return nil, nil, fmt.Errorf("stderr pipe: %w", err)
		}
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

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

	if m.journal != nil {
		pid := cmd.Process.Pid
		go journal.Collect(stdoutPipe, m.journal, u.Name, pid, journal.PriInfo)
		go journal.Collect(stderrPipe, m.journal, u.Name, pid, journal.PriError)
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
