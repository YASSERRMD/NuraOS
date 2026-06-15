//go:build linux && amd64

package seccomp_test

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/seccomp"
)

// childEnv is set in the subprocess to identify the test case to run.
const childEnv = "SECCOMP_TEST_CHILD"

func TestMain(m *testing.M) {
	// Subprocess mode: apply the filter and run the named test case.
	if tc := os.Getenv(childEnv); tc != "" {
		os.Exit(runChild(tc))
	}
	os.Exit(m.Run())
}

// runChild executes one of the child-side test cases inside a subprocess that
// has a seccomp filter active. We use a subprocess so the filter does not
// affect the main test binary.
func runChild(tc string) int {
	switch tc {
	case "deny-blocked":
		// Allow a small set; do NOT include getpid (nr=39).
		p := &seccomp.Profile{
			Syscalls: []string{
				"exit_group", "exit",
				"read", "write", "close", "fstat", "mmap",
				"brk", "rt_sigaction", "rt_sigprocmask",
				"futex", "set_tid_address", "set_robust_list",
				"arch_prctl", "sched_yield", "getrandom",
			},
		}
		if err := seccomp.Apply(p, seccomp.ModeEnforce); err != nil {
			os.Stderr.WriteString("apply: " + err.Error() + "\n")
			return 2
		}
		// getpid() should be blocked and return EPERM.
		_, _, errno := syscall.RawSyscall(syscall.SYS_GETPID, 0, 0, 0)
		if errno == syscall.EPERM {
			return 0 // expected
		}
		os.Stderr.WriteString("expected EPERM, got: " + errno.Error() + "\n")
		return 1

	case "allow-listed":
		// Allow getpid explicitly; it must succeed.
		p := &seccomp.Profile{
			Syscalls: []string{
				"exit_group", "exit",
				"read", "write", "close", "fstat", "mmap",
				"brk", "rt_sigaction", "rt_sigprocmask",
				"futex", "set_tid_address", "set_robust_list",
				"arch_prctl", "sched_yield", "getrandom",
				"getpid",
			},
		}
		if err := seccomp.Apply(p, seccomp.ModeEnforce); err != nil {
			os.Stderr.WriteString("apply: " + err.Error() + "\n")
			return 2
		}
		pid, _, errno := syscall.RawSyscall(syscall.SYS_GETPID, 0, 0, 0)
		if errno != 0 {
			os.Stderr.WriteString("getpid blocked unexpectedly: " + errno.Error() + "\n")
			return 1
		}
		if pid == 0 {
			os.Stderr.WriteString("getpid returned 0\n")
			return 1
		}
		return 0

	default:
		os.Stderr.WriteString("unknown child test case: " + tc + "\n")
		return 2
	}
}

// rerunAsChild re-executes the current test binary as a subprocess with
// childEnv set to the given test case name.
func rerunAsChild(t *testing.T, tc string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=TestMain")
	cmd.Env = append(os.Environ(), childEnv+"="+tc)
	return cmd
}

func TestDeniedSyscallIsBlocked(t *testing.T) {
	cmd := rerunAsChild(t, "deny-blocked")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child failed: %v\noutput: %s", err, out)
	}
}

func TestAllowedSyscallPasses(t *testing.T) {
	cmd := rerunAsChild(t, "allow-listed")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child failed: %v\noutput: %s", err, out)
	}
}

func TestProfileLoad(t *testing.T) {
	const tomlData = `syscalls = ["read", "write", "exit_group"]` + "\n"
	f, err := os.CreateTemp(t.TempDir(), "profile*.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(tomlData); err != nil {
		t.Fatal(err)
	}
	f.Close()

	p, err := seccomp.Load(f.Name())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(p.Syscalls) != 3 {
		t.Fatalf("expected 3 syscalls, got %d", len(p.Syscalls))
	}
}

func TestProfileLoadUnknownSyscall(t *testing.T) {
	const tomlData = `syscalls = ["read", "write", "totally_fake_syscall"]` + "\n"
	f, err := os.CreateTemp(t.TempDir(), "profile*.toml")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(tomlData)
	f.Close()

	p, err := seccomp.Load(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	err = seccomp.Apply(p, seccomp.ModeEnforce)
	if err == nil || !strings.Contains(err.Error(), "totally_fake_syscall") {
		t.Fatalf("expected error about unknown syscall, got %v", err)
	}
}
