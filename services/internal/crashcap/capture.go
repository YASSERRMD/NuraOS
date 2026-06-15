package crashcap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	// DefaultCrashDir is where crash bundles are written on the appliance.
	DefaultCrashDir = "/data/crashes"

	// MaxBundleSize is the maximum number of bytes written per crash bundle.
	// Larger captures are truncated from the top (oldest lines dropped).
	MaxBundleSize = 512 * 1024 // 512 KiB

	// MaxBundles is the maximum number of crash bundles retained before the
	// oldest are rotated out.
	MaxBundles = 20
)

// ServiceInfo describes the service that crashed.
type ServiceInfo struct {
	Name     string `json:"name"`
	PID      int    `json:"pid"`
	ExitCode int    `json:"exit_code"`
	Signal   string `json:"signal,omitempty"`
}

// ResourceSnapshot holds lightweight resource accounting at crash time.
type ResourceSnapshot struct {
	// MemUsageBytes is the RSS at crash time, read from /proc/<pid>/status.
	// Zero if not available.
	MemUsageBytes int64 `json:"mem_usage_bytes"`
	// CgroupSlice is the cgroup v2 path of the crashed process.
	CgroupSlice string `json:"cgroup_slice,omitempty"`
	// OpenFDs is the number of open file descriptors, from /proc/<pid>/fd.
	OpenFDs int `json:"open_fds"`
}

// Bundle is the structured capture written per crash event.
type Bundle struct {
	// CapturedAt is the UTC timestamp of capture.
	CapturedAt time.Time `json:"captured_at"`
	// Service describes the failed service.
	Service ServiceInfo `json:"service"`
	// Resources holds OS-level resource accounting.
	Resources ResourceSnapshot `json:"resources"`
	// LogTail contains the last N redacted log lines.
	LogTail []string `json:"log_tail"`
	// GoVersion is the Go runtime version, included for binary identification.
	GoVersion string `json:"go_version"`
}

// Capture writes a crash bundle for a failed service to crashDir and rotates
// old bundles if the count exceeds MaxBundles. Secrets are redacted before
// any data touches the filesystem.
func Capture(svc ServiceInfo, logTail []string, res ResourceSnapshot, crashDir string) (string, error) {
	if crashDir == "" {
		crashDir = DefaultCrashDir
	}
	if err := os.MkdirAll(crashDir, 0750); err != nil {
		return "", fmt.Errorf("crashcap: mkdir %s: %w", crashDir, err)
	}

	// Redact secrets from log lines before writing.
	redacted := make([]string, len(logTail))
	for i, line := range logTail {
		redacted[i] = RedactLine(line)
	}

	b := Bundle{
		CapturedAt: time.Now().UTC(),
		Service:    svc,
		Resources:  res,
		LogTail:    redacted,
		GoVersion:  runtime.Version(),
	}

	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", fmt.Errorf("crashcap: marshal: %w", err)
	}

	// Enforce per-bundle size cap by truncating the log tail if needed.
	if len(data) > MaxBundleSize {
		// Re-marshal with a shorter tail until it fits.
		for len(b.LogTail) > 0 && len(data) > MaxBundleSize {
			b.LogTail = b.LogTail[len(b.LogTail)/2:]
			data, _ = json.MarshalIndent(b, "", "  ")
		}
	}

	ts := b.CapturedAt.Format("20060102T150405Z")
	name := fmt.Sprintf("%s-%s.json", svc.Name, ts)
	path := filepath.Join(crashDir, name)

	if err := os.WriteFile(path, data, 0640); err != nil {
		return "", fmt.Errorf("crashcap: write %s: %w", path, err)
	}

	// Rotate: keep only the newest MaxBundles files.
	if err := rotate(crashDir, MaxBundles); err != nil {
		// Non-fatal: log but don't fail the capture.
		_ = err
	}

	return path, nil
}
