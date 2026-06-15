package subcmd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/resolver"
	"github.com/yasserrmd/nuraos/services/internal/unit"
)

// Run loads units from dir, resolves their order, and starts them in sequence.
// It runs until SIGTERM/SIGINT, then performs ordered shutdown.
func Run(dir string) error {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	units, err := unit.LoadDir(dir)
	if err != nil {
		return fmt.Errorf("load units: %w", err)
	}
	if len(units) == 0 {
		log.Warn("no enabled units found", "dir", dir)
		return nil
	}

	plan, err := resolver.Resolve(units)
	if err != nil {
		return fmt.Errorf("dependency resolution: %w", err)
	}

	log.Info("nura-manager starting", "units", len(plan.Order))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)

	// processes tracks running service processes keyed by unit name.
	processes := make(map[string]*serviceProc, len(plan.Order))
	var mu sync.Mutex

	// Start services in plan order, gating on readiness.
	for _, u := range plan.Order {
		log.Info("starting unit", "name", u.Name, "exec", u.Exec)

		proc := &serviceProc{unit: u, log: log}
		mu.Lock()
		processes[u.Name] = proc
		mu.Unlock()

		if err := proc.start(ctx); err != nil {
			log.Error("failed to start unit", "name", u.Name, "err", err)
			// Continue; other services should still start.
			continue
		}

		// Gate readiness before allowing dependants to start.
		if err := waitReady(ctx, u, log); err != nil {
			log.Warn("readiness timeout", "name", u.Name, "err", err)
			// Non-fatal: dependants may still start.
		}

		// For longrun/notify types, launch the restart supervisor in background.
		if u.Type != unit.TypeOneshot {
			go proc.restartLoop(ctx)
		}
	}

	log.Info("all units started; waiting for signal")

	// Block until shutdown signal.
	select {
	case sig := <-sigs:
		log.Info("received signal", "signal", sig)
	case <-ctx.Done():
	}

	log.Info("initiating ordered shutdown")

	// Shutdown in reverse start order.
	for i := len(plan.Order) - 1; i >= 0; i-- {
		u := plan.Order[i]
		mu.Lock()
		proc := processes[u.Name]
		mu.Unlock()
		if proc != nil {
			proc.stop(log)
		}
	}

	log.Info("nura-manager shutdown complete")
	return nil
}

// serviceProc holds the running state of a single service unit.
type serviceProc struct {
	unit *unit.Unit
	cmd  *exec.Cmd
	log  *slog.Logger
	mu   sync.Mutex
}

// start launches the service process once.
func (p *serviceProc) start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var parts []string
	if p.unit.User != "" && p.unit.User != "root" {
		// Wrap with su to drop to the declared user.
		parts = []string{"su", "-s", "/bin/sh", p.unit.User, "-c",
			shellJoin(p.unit.Exec, p.unit.Args)}
	} else {
		parts = append([]string{p.unit.Exec}, p.unit.Args...)
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return err
	}
	p.cmd = cmd
	p.log.Info("unit process started", "name", p.unit.Name, "pid", cmd.Process.Pid)
	return nil
}

// restartLoop monitors the process and restarts it per the unit restart policy.
func (p *serviceProc) restartLoop(ctx context.Context) {
	policy := p.unit.Restart.Policy
	backoff := time.Duration(p.unit.Restart.BackoffInit) * time.Second
	backoffMax := time.Duration(p.unit.Restart.BackoffMax) * time.Second
	crashTimes := make([]time.Time, 0, p.unit.Restart.CrashLoopLimit+1)

	for {
		p.mu.Lock()
		cmd := p.cmd
		p.mu.Unlock()
		if cmd == nil {
			return
		}

		err := cmd.Wait()
		if ctx.Err() != nil {
			return // context cancelled = normal shutdown
		}

		exitCode := 0
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		p.log.Info("unit exited", "name", p.unit.Name, "code", exitCode)

		// Determine whether to restart.
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
			p.log.Info("unit will not restart", "name", p.unit.Name, "policy", policy)
			return
		}

		// Crash-loop detection.
		now := time.Now()
		window := time.Duration(p.unit.Restart.CrashLoopWindow) * time.Second
		crashTimes = append(crashTimes, now)
		// Trim to window.
		cutoff := now.Add(-window)
		start := 0
		for start < len(crashTimes) && crashTimes[start].Before(cutoff) {
			start++
		}
		crashTimes = crashTimes[start:]

		if len(crashTimes) >= p.unit.Restart.CrashLoopLimit {
			pause := time.Duration(p.unit.Restart.CrashLoopBackoff) * time.Second
			p.log.Warn("crash loop detected, pausing",
				"name", p.unit.Name,
				"crashes", len(crashTimes),
				"pause", pause)
			select {
			case <-time.After(pause):
			case <-ctx.Done():
				return
			}
			crashTimes = crashTimes[:0]
			backoff = time.Duration(p.unit.Restart.BackoffInit) * time.Second
		}

		p.log.Info("restarting unit", "name", p.unit.Name, "backoff", backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		backoff *= 2
		if backoff > backoffMax {
			backoff = backoffMax
		}

		if err := p.start(ctx); err != nil {
			p.log.Error("restart failed", "name", p.unit.Name, "err", err)
		}
	}
}

// stop sends SIGTERM to the process and waits up to the stop timeout.
func (p *serviceProc) stop(log *slog.Logger) {
	p.mu.Lock()
	cmd := p.cmd
	p.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}

	log.Info("stopping unit", "name", p.unit.Name, "pid", cmd.Process.Pid)
	// Send SIGTERM to the process group.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	timeout := 15 * time.Second
	select {
	case <-done:
		log.Info("unit stopped cleanly", "name", p.unit.Name)
	case <-time.After(timeout):
		log.Warn("unit stop timeout, force-killing", "name", p.unit.Name)
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// waitReady waits for the unit to signal readiness per its probe config.
func waitReady(ctx context.Context, u *unit.Unit, log *slog.Logger) error {
	probe := u.Readiness
	timeout := time.Duration(probe.Timeout) * time.Second
	deadline := time.Now().Add(timeout)

	switch probe.Type {
	case unit.ReadinessHTTP:
		log.Info("waiting for HTTP readiness", "name", u.Name, "url", probe.URL)
		client := &http.Client{Timeout: 2 * time.Second}
		for time.Now().Before(deadline) {
			resp, err := client.Get(probe.URL)
			if err == nil && resp.StatusCode < 500 {
				resp.Body.Close()
				log.Info("unit ready", "name", u.Name)
				return nil
			}
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return fmt.Errorf("HTTP readiness timeout after %s", timeout)

	case unit.ReadinessSocket:
		log.Info("waiting for socket readiness", "name", u.Name, "socket", probe.Socket)
		for time.Now().Before(deadline) {
			c, err := net.DialTimeout("unix", probe.Socket, time.Second)
			if err == nil {
				c.Close()
				log.Info("unit ready", "name", u.Name)
				return nil
			}
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return fmt.Errorf("socket readiness timeout after %s", timeout)

	default:
		// TypeOneshot or ReadinessNone: no probe needed.
		return nil
	}
}

// shellJoin constructs a shell command string for use with su -c.
func shellJoin(exec string, args []string) string {
	parts := append([]string{exec}, args...)
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = "'" + strings.ReplaceAll(p, "'", "'\\''") + "'"
	}
	return strings.Join(quoted, " ")
}
