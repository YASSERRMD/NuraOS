package toolsandbox_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/toolsandbox"
)

// TestMain is the self-re-exec trampoline entry point.
// When NURA_SANDBOX_APPLY=1 the test binary acts as the sandbox trampoline:
// it applies kernel restrictions and exec's the actual tool. This pattern
// avoids a separate helper binary while keeping the enforcement code inline.
func TestMain(m *testing.M) {
	if toolsandbox.MaybeApplyAndExec() {
		// MaybeApplyAndExec only returns false when env var is absent.
		// The exec path replaces this process image and never returns here.
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func testRunner() *toolsandbox.Runner {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return toolsandbox.New(log, nil)
}

// TestGrantTypes verifies the Grant struct compiles and fields are accessible.
func TestGrantTypes(t *testing.T) {
	g := toolsandbox.Grant{
		Name:     "test-tool",
		Paths:    []toolsandbox.PathGrant{{Path: "/tmp", Read: true, Write: true}},
		Syscalls: []string{"read", "write", "exit", "exit_group"},
		MemLimit: 64 * 1024 * 1024,
		Timeout:  5 * time.Second,
	}
	if g.Name != "test-tool" {
		t.Errorf("Grant.Name = %q; want test-tool", g.Name)
	}
	if len(g.Paths) != 1 {
		t.Errorf("Grant.Paths len = %d; want 1", len(g.Paths))
	}
}

// TestErrNotSupportedOnNonLinux verifies that Run returns ErrNotSupported on
// platforms that don't support the OS sandbox (non-Linux).
// This test is skipped on Linux where Run is actually functional.
func TestRunOnNonLinuxReturnsNotSupported(t *testing.T) {
	if isLinux() {
		t.Skip("skipping non-Linux specific test on Linux")
	}
	r := testRunner()
	_, err := r.Run(context.Background(), toolsandbox.Grant{Name: "x"}, "/bin/true")
	if err != toolsandbox.ErrNotSupported {
		t.Errorf("err = %v; want ErrNotSupported", err)
	}
}
