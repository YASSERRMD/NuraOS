package diskmon_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/diskmon"
)

// TestDiskUsageSane checks that DiskUsage returns sane values for the current dir.
func TestDiskUsageSane(t *testing.T) {
	u, err := diskmon.DiskUsage(".")
	if err != nil {
		t.Fatalf("DiskUsage: %v", err)
	}
	if u.Total == 0 {
		t.Error("Total = 0; expected non-zero")
	}
	if u.UsedPct < 0 || u.UsedPct > 100 {
		t.Errorf("UsedPct %f out of [0, 100]", u.UsedPct)
	}
	if u.Used > u.Total {
		t.Errorf("Used %d > Total %d", u.Used, u.Total)
	}
}

// TestDiskUsageMissing checks that a missing path returns an error.
func TestDiskUsageMissing(t *testing.T) {
	_, err := diskmon.DiskUsage("/no-such-path-xyzzy-diskmon")
	if err == nil {
		t.Error("expected error for non-existent path, got nil")
	}
}

// TestMonitorInitialStatus verifies status is OK before any poll.
func TestMonitorInitialStatus(t *testing.T) {
	m := &diskmon.Monitor{
		Path:     ".",
		WarnPct:  80,
		Interval: time.Hour,
	}
	if s := m.CurrentStatus(); s != diskmon.StatusOK {
		t.Errorf("initial status = %v, want StatusOK", s)
	}
	if m.LastUsage() != nil {
		t.Error("LastUsage should be nil before first poll")
	}
}

// TestMonitorRunCancelled verifies Run exits promptly on context cancellation.
func TestMonitorRunCancelled(t *testing.T) {
	m := &diskmon.Monitor{
		Path:     ".",
		Interval: 50 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Error("Run did not exit after context cancellation")
	}
}

// TestMonitorLastUsageSet verifies LastUsage is non-nil after Run polls at least once.
func TestMonitorLastUsageSet(t *testing.T) {
	m := &diskmon.Monitor{
		Path:     ".",
		Interval: 10 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)
	// Give the first poll time to complete.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if m.LastUsage() != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("LastUsage still nil after 500ms")
}

// TestSubtreeUsageFiles returns the aggregate size of files in a temp dir.
func TestSubtreeUsageFiles(t *testing.T) {
	dir := t.TempDir()
	data := make([]byte, 512)
	if err := os.WriteFile(filepath.Join(dir, "a.bin"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.bin"), data, 0644); err != nil {
		t.Fatal(err)
	}
	used, err := diskmon.SubtreeUsage(dir)
	if err != nil {
		t.Fatalf("SubtreeUsage: %v", err)
	}
	if used < int64(len(data)*2) {
		t.Errorf("used = %d, want >= %d", used, len(data)*2)
	}
}

// TestSubtreeUsageEmpty returns 0 for an empty directory.
func TestSubtreeUsageEmpty(t *testing.T) {
	dir := t.TempDir()
	used, err := diskmon.SubtreeUsage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if used != 0 {
		t.Errorf("empty dir: used = %d, want 0", used)
	}
}

// TestQuotaCheckExceeded returns ok=false when usage exceeds the cap.
func TestQuotaCheckExceeded(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), make([]byte, 1024), 0644); err != nil {
		t.Fatal(err)
	}
	q := diskmon.Quota{Path: dir, MaxBytes: 100}
	used, ok, err := q.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if ok {
		t.Errorf("expected quota exceeded, got ok=true; used=%d", used)
	}
}

// TestQuotaUnlimited is always ok when MaxBytes == 0.
func TestQuotaUnlimited(t *testing.T) {
	q := diskmon.Quota{Path: ".", MaxBytes: 0}
	_, ok, err := q.Check()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("unlimited quota should always return ok=true")
	}
}

// TestReclaimTrimsOldest removes enough files to bring the subtree under cap.
func TestReclaimTrimsOldest(t *testing.T) {
	dataDir := t.TempDir()
	sessionDir := filepath.Join(dataDir, "sessions")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write 5 files of 100 bytes each (total 500 bytes); cap at 250.
	for i := 0; i < 5; i++ {
		name := filepath.Join(sessionDir, fmt.Sprintf("s%02d.json", i))
		if err := os.WriteFile(name, make([]byte, 100), 0644); err != nil {
			t.Fatal(err)
		}
	}

	freed, err := diskmon.Reclaim(diskmon.ReclaimOptions{
		DataDir:    dataDir,
		SessionCap: 250,
	})
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if freed == 0 {
		t.Error("expected bytes freed, got 0")
	}

	remaining, _ := diskmon.SubtreeUsage(sessionDir)
	if remaining > 250 {
		t.Errorf("after reclaim: used %d > cap 250", remaining)
	}
}

// TestReclaimNoop does nothing when the subtree is already under cap.
func TestReclaimNoop(t *testing.T) {
	dataDir := t.TempDir()
	sessionDir := filepath.Join(dataDir, "sessions")
	_ = os.MkdirAll(sessionDir, 0755)

	if err := os.WriteFile(filepath.Join(sessionDir, "tiny.json"), make([]byte, 10), 0644); err != nil {
		t.Fatal(err)
	}

	freed, _ := diskmon.Reclaim(diskmon.ReclaimOptions{DataDir: dataDir, SessionCap: 1024})
	if freed != 0 {
		t.Errorf("expected 0 freed (under cap), got %d", freed)
	}
}

// TestReclaimMissingDirNoPanic handles a non-existent sessions dir gracefully.
func TestReclaimMissingDirNoPanic(t *testing.T) {
	dataDir := t.TempDir()
	freed, err := diskmon.Reclaim(diskmon.ReclaimOptions{
		DataDir:    dataDir,
		SessionCap: 1024,
	})
	if err != nil {
		t.Fatalf("Reclaim on missing dir: %v", err)
	}
	if freed != 0 {
		t.Errorf("expected 0 freed for missing dir, got %d", freed)
	}
}

// TestStatusString exercises the Status.String method.
func TestStatusString(t *testing.T) {
	cases := []struct {
		s    diskmon.Status
		want string
	}{
		{diskmon.StatusOK, "ok"},
		{diskmon.StatusWarn, "warn"},
		{diskmon.StatusCritical, "critical"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("Status(%d).String() = %q, want %q", c.s, got, c.want)
		}
	}
}
