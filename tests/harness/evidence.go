package harness

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CaptureEvidence collects diagnostic data for a failing result and stores
// redacted copies in bundleBase/<suite>/<case>-evidence/. It updates the
// result's Evidence field in place. All captured text is redacted before
// being written to disk.
//
// Captured artifacts:
//   - serial.log -- full redacted serial console log
//   - metrics.txt -- GET /metrics response
//   - config.json -- GET /config response
//
// The last 100 lines of the serial log are embedded inline as JournalExcerpt.
func CaptureEvidence(ctx context.Context, inst *QEMUInstance, result *Result, bundleBase string) {
	dir := filepath.Join(bundleBase, result.Suite, result.Case+"-evidence")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	result.Evidence.BundleDir = dir

	if inst == nil {
		return
	}

	// Serial log -- already written by the background read loop.
	if serialDst := captureSerial(inst, dir); serialDst != "" {
		result.Evidence.SerialLogPath = serialDst
		result.Evidence.JournalExcerpt = tailLines(serialDst, 100)
	}

	// QEMU stderr -- captures QEMU startup errors and KVM/TCG fallback messages.
	captureFile(inst.StderrLogPath, filepath.Join(dir, "qemu-stderr.log"))

	// ttyS1 direct-file serial -- written by QEMU without a socket connection.
	// Non-zero size here when serial.log is empty proves the unix socket is
	// losing data; both empty proves the kernel is not writing to any UART.
	captureFile(inst.EarlySerialLogPath, filepath.Join(dir, "early-serial.log"))

	// Metrics snapshot.
	if path := captureEndpoint(ctx, inst, "/metrics", filepath.Join(dir, "metrics.txt")); path != "" {
		result.Evidence.MetricsSnapshot = path
	}

	// Config dump.
	if path := captureEndpoint(ctx, inst, "/config", filepath.Join(dir, "config.json")); path != "" {
		result.Evidence.ConfigDump = path
	}
}

// FinaliseResults stamps run context on every result, redacts messages, and
// for failures captures evidence and computes the failure signature.
// Pass inst=nil when no QEMU instance is available (e.g. boot failed).
func FinaliseResults(ctx context.Context, rc *RunContext, results []Result, inst *QEMUInstance, bundleBase string) {
	for i := range results {
		r := &results[i]
		rc.Apply(r)
		r.Message = Redact(r.Message)
		if r.Status == StatusFail {
			CaptureEvidence(ctx, inst, r, bundleBase)
			r.FailureSignature = FailureSignature(r.Suite, r.Case, r.Message)
		}
	}
}

// captureSerial copies the serial log to the evidence bundle (redacted) and
// returns the path to the copied file. Returns empty on error.
func captureSerial(inst *QEMUInstance, dir string) string {
	if inst.SerialLogPath == "" {
		return ""
	}
	data, err := os.ReadFile(inst.SerialLogPath)
	if err != nil {
		return ""
	}
	dst := filepath.Join(dir, "serial.log")
	if err := os.WriteFile(dst, []byte(Redact(string(data))), 0o644); err != nil {
		return ""
	}
	return dst
}

// captureFile copies src to dst verbatim (no redaction). Silently returns on error.
func captureFile(src, dst string) {
	if src == "" {
		return
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	_ = os.WriteFile(dst, data, 0o644)
}

// captureEndpoint GETs an endpoint on the guest, redacts the response, and
// saves it to path. Returns the path on success, empty string on any error.
func captureEndpoint(ctx context.Context, inst *QEMUInstance, endpoint, path string) string {
	_ = ctx
	code, body, err := inst.HTTP().GetBody(endpoint)
	if err != nil || code != 200 {
		return ""
	}
	if err := os.WriteFile(path, []byte(Redact(body)), 0o644); err != nil {
		return ""
	}
	return path
}

// tailLines reads the file at path and returns the last n lines as a string.
func tailLines(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
