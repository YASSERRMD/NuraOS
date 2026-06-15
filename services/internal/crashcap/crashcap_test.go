package crashcap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/crashcap"
)

// TestRedactLineRemovesTokens verifies known secret patterns are redacted.
func TestRedactLineRemovesTokens(t *testing.T) {
	cases := []struct {
		input    string
		mustSkip string // substring that MUST NOT appear in output
	}{
		{`TOKEN=supersecret123`, `supersecret123`},
		{`api_key: abc123def456`, `abc123def456`},
		{`Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.xyz`, `eyJhbGciOiJIUzI1NiJ9`},
		{`password=hunter2`, `hunter2`},
	}
	for _, tc := range cases {
		out := crashcap.RedactLine(tc.input)
		if strings.Contains(out, tc.mustSkip) {
			t.Errorf("RedactLine(%q) = %q; still contains %q", tc.input, out, tc.mustSkip)
		}
		if !strings.Contains(out, "[REDACTED]") {
			t.Errorf("RedactLine(%q) = %q; missing [REDACTED] marker", tc.input, out)
		}
	}
}

// TestRedactLineSafeInput verifies benign lines are not changed.
func TestRedactLineSafeInput(t *testing.T) {
	line := "gateway started on port 8080"
	out := crashcap.RedactLine(line)
	if out != line {
		t.Errorf("RedactLine modified safe line: got %q; want %q", out, line)
	}
}

// TestCaptureWritesFile verifies Capture creates a JSON file in the crash dir.
func TestCaptureWritesFile(t *testing.T) {
	dir := t.TempDir()
	svc := crashcap.ServiceInfo{Name: "gateway", PID: 1234, ExitCode: 1}
	logTail := []string{"starting up", "api_key=topsecret", "segfault"}

	path, err := crashcap.Capture(svc, logTail, crashcap.ResourceSnapshot{}, dir)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("capture file not found: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if strings.Contains(content, "topsecret") {
		t.Errorf("capture file contains unredacted secret 'topsecret'")
	}
	if !strings.Contains(content, "[REDACTED]") {
		t.Errorf("capture file missing [REDACTED] placeholder")
	}
}

// TestCaptureFileNameContainsServiceName verifies the filename encodes the service.
func TestCaptureFileNameContainsServiceName(t *testing.T) {
	dir := t.TempDir()
	svc := crashcap.ServiceInfo{Name: "llama-server", ExitCode: 137}
	path, err := crashcap.Capture(svc, nil, crashcap.ResourceSnapshot{}, dir)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "llama-server-") {
		t.Errorf("filename %q does not start with service name 'llama-server-'", base)
	}
}

// TestRotateKeepsOnlyN verifies old bundles are pruned after MaxBundles are written.
func TestRotateKeepsOnlyN(t *testing.T) {
	dir := t.TempDir()
	svc := crashcap.ServiceInfo{Name: "svc", ExitCode: 1}

	// Write more than MaxBundles captures.
	limit := crashcap.MaxBundles + 5
	for i := 0; i < limit; i++ {
		if _, err := crashcap.Capture(svc, nil, crashcap.ResourceSnapshot{}, dir); err != nil {
			t.Fatalf("Capture %d: %v", i, err)
		}
		// Sleep a tiny bit so timestamps differ.
		time.Sleep(2 * time.Millisecond)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			files = append(files, e.Name())
		}
	}
	if len(files) > crashcap.MaxBundles {
		t.Errorf("after rotation: %d files remain; want <= %d", len(files), crashcap.MaxBundles)
	}
}
