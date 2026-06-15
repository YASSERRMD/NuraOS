package netstat

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleProcNetDev = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:   12345      80    0    0    0     0          0         0   12345      80    0    0    0     0       0          0
  eth0: 9876543    5678    2    1    0     0          0         0 1234567    4321    0    0    0     0       0          0
`

func writeSample(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "net_dev")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

// TestParseSampleFile verifies parsing of a representative /proc/net/dev layout.
func TestParseSampleFile(t *testing.T) {
	path := writeSample(t, sampleProcNetDev)
	stats, err := readFrom(path)
	if err != nil {
		t.Fatalf("readFrom: %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("len(stats) = %d; want 2", len(stats))
	}

	lo := stats[0]
	if lo.Name != "lo" {
		t.Errorf("stats[0].Name = %q; want lo", lo.Name)
	}
	if lo.RxBytes != 12345 {
		t.Errorf("lo.RxBytes = %d; want 12345", lo.RxBytes)
	}
	if lo.TxBytes != 12345 {
		t.Errorf("lo.TxBytes = %d; want 12345", lo.TxBytes)
	}

	eth := stats[1]
	if eth.Name != "eth0" {
		t.Errorf("stats[1].Name = %q; want eth0", eth.Name)
	}
	if eth.RxBytes != 9876543 {
		t.Errorf("eth0.RxBytes = %d; want 9876543", eth.RxBytes)
	}
	if eth.TxBytes != 1234567 {
		t.Errorf("eth0.TxBytes = %d; want 1234567", eth.TxBytes)
	}
	if eth.RxErrs != 2 {
		t.Errorf("eth0.RxErrs = %d; want 2", eth.RxErrs)
	}
	if eth.RxDrop != 1 {
		t.Errorf("eth0.RxDrop = %d; want 1", eth.RxDrop)
	}
}

// TestEmptyFile verifies parsing an empty file returns empty stats.
func TestEmptyFile(t *testing.T) {
	path := writeSample(t, "Inter-| ...\n face | ...\n")
	stats, err := readFrom(path)
	if err != nil {
		t.Fatalf("readFrom: %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("len(stats) = %d; want 0", len(stats))
	}
}

// TestMissingFile verifies that a missing path returns an error, not a panic.
func TestMissingFile(t *testing.T) {
	_, err := readFrom("/nonexistent/path/net_dev")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}
