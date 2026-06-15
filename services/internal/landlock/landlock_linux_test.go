//go:build linux && amd64

package landlock_test

import (
	"os"
	"os/exec"
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/landlock"
)

const childEnv = "LANDLOCK_TEST_CHILD"

func TestMain(m *testing.M) {
	if tc := os.Getenv(childEnv); tc != "" {
		os.Exit(runChild(tc))
	}
	os.Exit(m.Run())
}

func runChild(tc string) int {
	switch tc {
	case "deny-etc":
		// Allow only /tmp; /etc should be inaccessible.
		p := &landlock.Profile{
			Paths: []landlock.PathRule{
				{Path: "/tmp", Access: []landlock.Access{
					landlock.AccessReadFile, landlock.AccessWriteFile,
					landlock.AccessReadDir, landlock.AccessMakeReg,
					landlock.AccessRemoveFile,
				}},
			},
		}
		if err := landlock.Apply(p); err != nil {
			// If the kernel doesn't support Landlock, skip rather than fail.
			os.Stdout.WriteString("skip: " + err.Error() + "\n")
			return 42 // special exit code: "skipped"
		}
		// Try to read /etc/hostname - must fail with EACCES or ENOENT (no access).
		_, err := os.ReadFile("/etc/hostname")
		if err == nil {
			os.Stderr.WriteString("expected denied access to /etc/hostname, got nil error\n")
			return 1
		}
		return 0 // correctly denied

	case "allow-tmp":
		// Allow /tmp; writing there must succeed.
		p := &landlock.Profile{
			Paths: []landlock.PathRule{
				{Path: "/tmp", Access: []landlock.Access{
					landlock.AccessReadFile, landlock.AccessWriteFile,
					landlock.AccessReadDir, landlock.AccessMakeReg,
					landlock.AccessRemoveFile,
				}},
			},
		}
		if err := landlock.Apply(p); err != nil {
			os.Stdout.WriteString("skip: " + err.Error() + "\n")
			return 42
		}
		f, err := os.CreateTemp("/tmp", "landlock-test-*")
		if err != nil {
			os.Stderr.WriteString("create temp in /tmp failed: " + err.Error() + "\n")
			return 1
		}
		f.Close()
		os.Remove(f.Name())
		return 0

	default:
		os.Stderr.WriteString("unknown child test: " + tc + "\n")
		return 2
	}
}

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

func TestLandlockDeniesUnlistedPath(t *testing.T) {
	cmd := rerunAsChild(t, "deny-etc")
	out, err := cmd.CombinedOutput()
	if cmd.ProcessState.ExitCode() == 42 {
		t.Skip("kernel does not support Landlock; skipping")
	}
	if err != nil {
		t.Fatalf("child failed: %v\noutput: %s", err, out)
	}
}

func TestLandlockAllowsListedPath(t *testing.T) {
	cmd := rerunAsChild(t, "allow-tmp")
	out, err := cmd.CombinedOutput()
	if cmd.ProcessState.ExitCode() == 42 {
		t.Skip("kernel does not support Landlock; skipping")
	}
	if err != nil {
		t.Fatalf("child failed: %v\noutput: %s", err, out)
	}
}

func TestProfileLoad(t *testing.T) {
	const tomlData = `
[[paths]]
path = "/data/logs"
access = ["read_file", "write_file", "read_dir"]

[[paths]]
path = "/etc/nura"
access = ["read_file", "read_dir"]
`
	f, err := os.CreateTemp(t.TempDir(), "profile*.toml")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(tomlData)
	f.Close()

	p, err := landlock.Load(f.Name())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(p.Paths) != 2 {
		t.Fatalf("expected 2 paths, got %d", len(p.Paths))
	}
	if p.Paths[0].Path != "/data/logs" {
		t.Errorf("path[0] = %q; want /data/logs", p.Paths[0].Path)
	}
}
