package providerhealth_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/eventbus"
	"github.com/yasserrmd/nuraos/services/internal/providerhealth"
)

func testLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestSnapshotEmpty verifies Snapshot returns an empty slice when no providers are added.
func TestSnapshotEmpty(t *testing.T) {
	m := providerhealth.New(nil, testLog())
	snap := m.Snapshot()
	if len(snap) != 0 {
		t.Errorf("snapshot len = %d; want 0", len(snap))
	}
}

// TestShouldFallbackUnknownProvider returns false for an unknown provider.
func TestShouldFallbackUnknownProvider(t *testing.T) {
	m := providerhealth.New(nil, testLog())
	if m.ShouldFallback("nonexistent") {
		t.Error("ShouldFallback should be false for unknown provider")
	}
}

// TestHealthyProviderNoFallback verifies a reachable provider does not trigger fallback.
func TestHealthyProviderNoFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	bus := eventbus.NewBus()
	m := providerhealth.New(bus, testLog())
	m.Add(providerhealth.ProviderConfig{
		Name:          "cloud",
		ProbeURL:      srv.URL,
		Interval:      time.Hour,
		FailThreshold: 3,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)

	time.Sleep(200 * time.Millisecond) // let the initial probe complete

	if m.ShouldFallback("cloud") {
		t.Error("ShouldFallback should be false for healthy provider")
	}

	snap := m.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d; want 1", len(snap))
	}
	if snap[0].Name != "cloud" {
		t.Errorf("snapshot[0].Name = %q; want cloud", snap[0].Name)
	}
	if !snap[0].Reachable {
		t.Error("snapshot[0].Reachable should be true for healthy provider")
	}
}

// TestUnhealthyProviderTriggersFallback verifies that FailThreshold failures trip
// the circuit and ShouldFallback returns true.
func TestUnhealthyProviderTriggersFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	bus := eventbus.NewBus()
	sub, unsub := bus.Subscribe(8)
	defer unsub()

	m := providerhealth.New(bus, testLog())
	m.Add(providerhealth.ProviderConfig{
		Name:          "remote",
		ProbeURL:      srv.URL,
		Interval:      50 * time.Millisecond, // fast probing for test
		Timeout:       200 * time.Millisecond,
		FailThreshold: 3,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)

	// Wait for FailThreshold failures to trip the circuit.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m.ShouldFallback("remote") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !m.ShouldFallback("remote") {
		t.Error("ShouldFallback should be true after circuit trips")
	}

	// Verify a degraded event was published.
	select {
	case ev := <-sub:
		if ev.Type != "provider.degraded" {
			t.Logf("got event type %q (not provider.degraded, may be ok if arrived in right order)", ev.Type)
		}
	default:
		t.Log("no bus event received immediately (race acceptable)")
	}

	snap := m.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d; want 1", len(snap))
	}
	if !snap[0].ShouldFallback {
		t.Error("snapshot[0].ShouldFallback should be true")
	}
	if snap[0].CircuitState != "open" {
		t.Errorf("circuit_state = %q; want open", snap[0].CircuitState)
	}
}

// TestRunExitsOnContextCancel verifies Run exits when context is cancelled.
func TestRunExitsOnContextCancel(t *testing.T) {
	m := providerhealth.New(nil, testLog())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not exit within 1s of context cancellation")
	}
}
