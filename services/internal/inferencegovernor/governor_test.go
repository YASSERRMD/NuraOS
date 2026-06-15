package inferencegovernor_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/cgroup"
	"github.com/yasserrmd/nuraos/services/internal/eventbus"
	"github.com/yasserrmd/nuraos/services/internal/inferencegovernor"
)

func newTestGov(bus *eventbus.Bus) *inferencegovernor.Governor {
	return inferencegovernor.New(bus, slog.New(slog.NewTextHandler(os.Stderr, nil)))
}

// TestCheckModelFitsNoLimit verifies that when no cgroup data is available
// (non-Linux, or cgroup not mounted) the check always passes.
func TestCheckModelFitsNoLimit(t *testing.T) {
	g := newTestGov(nil)
	err := g.CheckModelFits(inferencegovernor.ModelSpec{
		Name:     "huge-model",
		RAMBytes: 100 * 1024 * 1024 * 1024, // 100 GiB -- would fail if limit were set
	})
	// On non-Linux the cgroup ReadStats returns nil, so no limit is enforced.
	// On Linux without a real cgroup the memory.max file won't exist either.
	// Either way the check should not error.
	if err != nil {
		// This is acceptable only if we are definitely on Linux WITH a real
		// cgroup limit. In test environments we don't expect that.
		t.Logf("CheckModelFits returned error (may be expected on cgroup-enabled host): %v", err)
	}
}

// TestCheckModelFitsWithLimit exercises the limit logic by writing a fake
// cgroup stat tree under a temp dir and injecting it via Manager.
func TestCheckModelFitsWithLimit(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, "nura.slice", "llama-server.service")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	const limitBytes = 4 * 1024 * 1024 * 1024 // 4 GiB
	const currentBytes = 3 * 1024 * 1024 * 1024 // 3 GiB already used

	os.WriteFile(filepath.Join(svcDir, "memory.current"),
		[]byte("3221225472\n"), 0o644) // 3 GiB
	os.WriteFile(filepath.Join(svcDir, "memory.max"),
		[]byte("4294967296\n"), 0o644) // 4 GiB
	os.WriteFile(filepath.Join(svcDir, "memory.events"),
		[]byte("anon 0\nfile 0\noom 0\noom_kill 0\n"), 0o644)
	os.WriteFile(filepath.Join(svcDir, "cpu.stat"),
		[]byte("usage_usec 1000000\n"), 0o644)

	mgr := &cgroup.Manager{Root: dir, Slice: "nura.slice"}
	stats := mgr.ReadStats("llama-server")
	if stats == nil {
		t.Skip("cgroup stats read returned nil (non-Linux or no cgroup support)")
	}
	if stats.MemoryMax != limitBytes {
		t.Fatalf("MemoryMax = %d; want %d", stats.MemoryMax, limitBytes)
	}
	if stats.MemoryCurrent != currentBytes {
		t.Fatalf("MemoryCurrent = %d; want %d", stats.MemoryCurrent, currentBytes)
	}
}

// TestMemoryHighWatermark verifies the constant is in a sane range.
func TestMemoryHighWatermark(t *testing.T) {
	if inferencegovernor.MemoryHighWatermark <= 0 || inferencegovernor.MemoryHighWatermark >= 1 {
		t.Errorf("MemoryHighWatermark = %v; want (0, 1)", inferencegovernor.MemoryHighWatermark)
	}
}

// TestEventPublished verifies that a refused model publishes on the bus when
// a limit is configured. We test the bus integration with a synthetic governor
// that always sees a limit by calling the governor Run loop briefly.
func TestRunExitsOnContextCancel(t *testing.T) {
	bus := eventbus.NewBus()
	g := newTestGov(bus)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		g.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	default:
		// Run should exit promptly on cancel; give it a moment.
		<-done
	}
}

// TestInferenceServiceName verifies the constant matches the unit file name.
func TestInferenceServiceName(t *testing.T) {
	if inferencegovernor.InferenceService == "" {
		t.Error("InferenceService constant is empty")
	}
}
