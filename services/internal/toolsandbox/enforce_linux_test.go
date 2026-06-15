//go:build linux

package toolsandbox_test

// enforce_linux_test.go contains tests that verify the OS-level sandbox
// denies out-of-scope access. These tests require Linux and run as part of
// the standard test suite on the target system. They may require root or
// CAP_SYS_ADMIN for namespace isolation (they still pass without it as the
// trampoline applies Landlock and seccomp before exec, regardless of namespaces).

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/toolsandbox"
)

// TestSandboxedEchoSucceeds verifies a basic command (echo) succeeds inside
// the sandbox when it has read+exec access to /bin and /usr.
func TestSandboxedEchoSucceeds(t *testing.T) {
	echo, err := findBin("echo")
	if err != nil {
		t.Skipf("echo not found: %v", err)
	}

	r := testRunner()
	res, err := r.Run(context.Background(), toolsandbox.Grant{
		Name: "echo-test",
		Paths: []toolsandbox.PathGrant{
			{Path: "/", Read: true},
			{Path: "/bin", Read: true, Exec: true},
			{Path: "/usr", Read: true, Exec: true},
			{Path: "/lib", Read: true, Exec: true},
			{Path: "/lib64", Read: true, Exec: true},
		},
		Timeout: 5 * time.Second,
	}, echo, "hello-sandbox")

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit code = %d; want 0. stderr: %s", res.ExitCode, res.Stderr)
	}
	if !bytes.Contains(res.Stdout, []byte("hello-sandbox")) {
		t.Errorf("stdout = %q; want to contain hello-sandbox", res.Stdout)
	}
}

// TestTimeoutKillsTool verifies that the wall-clock timeout kills a hung tool.
func TestTimeoutKillsTool(t *testing.T) {
	sleep, err := findBin("sleep")
	if err != nil {
		t.Skipf("sleep not found: %v", err)
	}

	r := testRunner()
	res, err := r.Run(context.Background(), toolsandbox.Grant{
		Name:    "sleep-timeout",
		Timeout: 200 * time.Millisecond,
		Paths: []toolsandbox.PathGrant{
			{Path: "/", Read: true},
			{Path: "/bin", Read: true, Exec: true},
			{Path: "/usr", Read: true, Exec: true},
			{Path: "/lib", Read: true, Exec: true},
			{Path: "/lib64", Read: true, Exec: true},
		},
	}, sleep, "60")

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !res.Killed {
		t.Errorf("expected tool to be killed by timeout; exit code = %d", res.ExitCode)
	}
}

// TestWriteBlockedByLandlock verifies that a tool cannot write to a directory
// not listed in its grant. This test requires Linux >= 5.13 with Landlock support.
// On older kernels the test is skipped (Landlock Apply returns an error).
func TestWriteBlockedByLandlock(t *testing.T) {
	sh, err := findBin("sh")
	if err != nil {
		t.Skipf("sh not found: %v", err)
	}

	// Create a target directory the tool should NOT be able to write to.
	deny, err := os.MkdirTemp("", "sandbox-deny-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(deny)

	// Grant only read access to /tmp (not the deny dir) and exec access to /bin.
	r := testRunner()
	res, err := r.Run(context.Background(), toolsandbox.Grant{
		Name: "write-blocked",
		Paths: []toolsandbox.PathGrant{
			{Path: "/", Read: true},
			{Path: "/bin", Read: true, Exec: true},
			{Path: "/usr", Read: true, Exec: true},
			{Path: "/lib", Read: true, Exec: true},
			{Path: "/lib64", Read: true, Exec: true},
			// deny dir is intentionally NOT listed
		},
		Timeout: 5 * time.Second,
	}, sh, "-c", "echo test > "+filepath.Join(deny, "out.txt")+"; echo done")

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Check if the file was NOT created (Landlock denied the write).
	if _, statErr := os.Stat(filepath.Join(deny, "out.txt")); statErr == nil {
		// File was created - Landlock was not applied (old kernel).
		// Check if the trampoline reported skipping Landlock.
		if strings.Contains(string(res.Stderr), "landlock skipped") {
			t.Skip("Landlock not supported by running kernel; skipping enforcement check")
		}
		t.Error("file was written to ungranrted directory; Landlock did not deny access")
	}
	// File does not exist: Landlock correctly denied the write.
}

// findBin searches common bin directories for a binary.
func findBin(name string) (string, error) {
	for _, dir := range []string{"/bin", "/usr/bin"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", os.ErrNotExist
}
