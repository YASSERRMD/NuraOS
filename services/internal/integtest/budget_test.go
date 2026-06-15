package integtest_test

import (
	"os"
	"runtime"
	"testing"
)

// maxHeapMB is the soft upper bound for heap in use during the integration
// matrix. The matrix itself is lightweight; if this fires, a scenario is
// leaking memory.
const maxHeapMB = 64

// maxGoroutines is the upper bound on goroutines after the matrix completes.
const maxGoroutines = 128

// TestBudgetMemoryFootprint asserts the test process heap stays below maxHeapMB
// after a full matrix run.
func TestBudgetMemoryFootprint(t *testing.T) {
	if os.Getenv("NURA_SKIP_BUDGET") != "" {
		t.Skip("NURA_SKIP_BUDGET set")
	}
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	heapMB := ms.HeapInuse / (1024 * 1024)
	if heapMB > maxHeapMB {
		t.Errorf("heap in use = %d MiB; want < %d MiB", heapMB, maxHeapMB)
	}
}

// TestBudgetGoroutineCount asserts no goroutines leaked during the matrix.
func TestBudgetGoroutineCount(t *testing.T) {
	if os.Getenv("NURA_SKIP_BUDGET") != "" {
		t.Skip("NURA_SKIP_BUDGET set")
	}
	n := runtime.NumGoroutine()
	if n > maxGoroutines {
		t.Errorf("goroutine count = %d; want < %d (possible leak)", n, maxGoroutines)
	}
}
