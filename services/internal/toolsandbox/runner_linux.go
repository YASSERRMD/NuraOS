//go:build linux

package toolsandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/eventbus"
	"github.com/yasserrmd/nuraos/services/internal/unit"
)

// Run executes the tool at toolPath with args inside the sandbox defined by
// grant. On return, Result contains the exit code, stdout, stderr, and whether
// the process was killed. An error is returned only when the runner itself fails
// (e.g., cannot fork, cgroup creation failed); a non-zero tool exit code is
// reported in Result.ExitCode, not as an error.
//
// The caller must ensure the running binary handles MaybeApplyAndExec() at
// startup (i.e., at the top of main() or TestMain). The sandbox uses the
// running binary as the trampoline: it re-execs itself with NURA_SANDBOX_APPLY=1
// to apply kernel restrictions before exec'ing the actual tool.
//
// Namespace isolation requires root or CAP_SYS_ADMIN. If the caller lacks
// privileges, Run falls back to launching the tool directly with Landlock and
// seccomp applied in the trampoline (no namespace isolation).
func (r *Runner) Run(ctx context.Context, grant Grant, toolPath string, args ...string) (Result, error) {
	if grant.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, grant.Timeout)
		defer cancel()
	}

	// Encode the grant for the trampoline.
	grantJSON, err := json.Marshal(grant)
	if err != nil {
		return Result{}, fmt.Errorf("toolsandbox: marshal grant: %w", err)
	}

	// Find the current binary to use as the trampoline.
	self, err := os.Executable()
	if err != nil {
		return Result{}, fmt.Errorf("toolsandbox: cannot find executable: %w", err)
	}
	if envBin := os.Getenv("NURA_SANDBOX_BIN"); envBin != "" {
		self = envBin
	}

	// Build argv: <self> --nura-sandbox -- <toolPath> <args...>
	argv := append([]string{"--nura-sandbox", "--", toolPath}, args...)
	cmd := exec.CommandContext(ctx, self, argv...)
	cmd.Env = []string{
		"NURA_SANDBOX_APPLY=1",
		"NURA_SANDBOX_GRANT=" + string(grantJSON),
		"PATH=/usr/bin:/bin",
	}

	// Namespace isolation: new mount and IPC namespaces.
	// Requires CAP_SYS_ADMIN; fails gracefully if unprivileged.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWIPC,
		NoNewPrivs: true,
		Pdeathsig:  syscall.SIGKILL,
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set up cgroup resource limits before starting the child.
	cgName := cgroupName(grant.Name)
	if r.cgMgr != nil && (grant.MemLimit > 0 || grant.CPUWeight > 0) {
		res := &unit.Resources{}
		if grant.CPUWeight > 0 {
			res.CPUWeight = grant.CPUWeight
		}
		if grant.MemLimit > 0 {
			res.MemoryMax = fmt.Sprintf("%d", grant.MemLimit)
		}
		if err := r.cgMgr.Create(cgName, res); err != nil {
			r.log.Warn("toolsandbox: cgroup create failed",
				"tool", grant.Name, "err", err)
		} else {
			defer func() { _ = r.cgMgr.Delete(cgName) }()
		}
	}

	startTime := time.Now()
	if err := cmd.Start(); err != nil {
		// If namespace creation failed due to privileges, retry without namespaces.
		if isPermError(err) {
			r.log.Warn("toolsandbox: namespace isolation unavailable; retrying without namespaces",
				"tool", grant.Name, "err", err)
			cmd2 := exec.CommandContext(ctx, self, argv...)
			cmd2.Env = cmd.Env
			cmd2.SysProcAttr = &syscall.SysProcAttr{NoNewPrivs: true}
			cmd2.Stdout = &stdout
			cmd2.Stderr = &stderr
			if err2 := cmd2.Start(); err2 != nil {
				return Result{}, fmt.Errorf("toolsandbox: start failed: %w", err2)
			}
			cmd = cmd2
		} else {
			return Result{}, fmt.Errorf("toolsandbox: start failed: %w", err)
		}
	}

	// Add to cgroup after start (small race is acceptable).
	if r.cgMgr != nil && cmd.Process != nil {
		_ = r.cgMgr.AddPid(cgName, cmd.Process.Pid)
	}

	r.log.Info("toolsandbox: tool started",
		"tool", grant.Name, "pid", cmd.Process.Pid, "path", toolPath)

	waitErr := cmd.Wait()
	elapsed := time.Since(startTime)

	res := Result{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
	}

	if ctx.Err() != nil {
		res.Killed = true
		res.ExitCode = -1
	} else if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			res.ExitCode = exitErr.ExitCode()
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				res.Killed = true
			}
		} else {
			return Result{}, fmt.Errorf("toolsandbox: wait failed: %w", waitErr)
		}
	}

	r.log.Info("toolsandbox: tool finished",
		"tool", grant.Name, "exit", res.ExitCode, "elapsed", elapsed, "killed", res.Killed)

	if r.bus != nil {
		evType := eventbus.TypeServiceStopped
		if res.Killed {
			evType = eventbus.TypeServiceFailed
		}
		r.bus.Publish(eventbus.NewEvent(evType, "toolsandbox", map[string]any{
			"tool":     grant.Name,
			"exit":     res.ExitCode,
			"elapsed":  elapsed.String(),
			"killed":   res.Killed,
		}))
	}

	return res, nil
}

// cgroupName converts a tool name into a cgroup-safe identifier.
var reCGUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func cgroupName(name string) string {
	safe := reCGUnsafe.ReplaceAllString(name, "-")
	safe = strings.TrimLeft(safe, "-")
	if safe == "" {
		safe = "tool"
	}
	return "tool-" + safe
}

// isPermError returns true when err indicates a privilege/permission failure.
func isPermError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "operation not permitted") ||
		strings.Contains(s, "permission denied")
}

// slog methods need log to be non-nil; wire nop logger if not provided.
func (r *Runner) logger() *slog.Logger {
	if r.log != nil {
		return r.log
	}
	return slog.Default()
}
