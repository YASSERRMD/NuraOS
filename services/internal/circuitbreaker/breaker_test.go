package circuitbreaker_test

import (
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/circuitbreaker"
	"github.com/yasserrmd/nuraos/services/internal/eventbus"
)

func newBreaker(failThresh, recoverThresh int, openDur time.Duration) *circuitbreaker.Breaker {
	return circuitbreaker.New(circuitbreaker.Config{
		Name:             "test-provider",
		FailThreshold:    failThresh,
		RecoverThreshold: recoverThresh,
		OpenDuration:     openDur,
	}, nil)
}

// TestInitialStateClosed verifies the breaker starts Closed.
func TestInitialStateClosed(t *testing.T) {
	b := newBreaker(3, 2, time.Minute)
	if got := b.State(); got != circuitbreaker.StateClosed {
		t.Errorf("initial state = %v; want Closed", got)
	}
	if !b.Allow() {
		t.Error("Allow() should return true when Closed")
	}
}

// TestTripOnConsecutiveFailures verifies that FailThreshold failures trip the breaker.
func TestTripOnConsecutiveFailures(t *testing.T) {
	b := newBreaker(3, 2, time.Minute)
	for i := 0; i < 2; i++ {
		b.Record(false)
		if b.State() != circuitbreaker.StateClosed {
			t.Fatalf("state after %d failures = %v; want Closed", i+1, b.State())
		}
	}
	b.Record(false) // 3rd failure trips
	if b.State() != circuitbreaker.StateOpen {
		t.Errorf("state after 3 failures = %v; want Open", b.State())
	}
	if b.Allow() {
		t.Error("Allow() should return false when Open")
	}
}

// TestSuccessResetsFailCount verifies that a success resets the failure counter.
func TestSuccessResetsFailCount(t *testing.T) {
	b := newBreaker(3, 2, time.Minute)
	b.Record(false)
	b.Record(false)
	b.Record(true) // success resets fail count
	b.Record(false)
	b.Record(false)
	// only 2 failures since last success; should still be Closed
	if b.State() != circuitbreaker.StateClosed {
		t.Errorf("state = %v; want Closed after reset", b.State())
	}
}

// TestHalfOpenTransitionAfterCooldown verifies Open transitions to HalfOpen
// once OpenDuration has elapsed.
func TestHalfOpenTransitionAfterCooldown(t *testing.T) {
	b := circuitbreaker.New(circuitbreaker.Config{
		Name:             "p",
		FailThreshold:    1,
		RecoverThreshold: 1,
		OpenDuration:     1 * time.Millisecond,
	}, nil)
	b.Record(false) // trip
	if b.State() != circuitbreaker.StateOpen {
		t.Fatalf("expected Open after trip")
	}
	time.Sleep(5 * time.Millisecond) // wait for cooldown
	if got := b.State(); got != circuitbreaker.StateHalfOpen {
		t.Errorf("state after cooldown = %v; want HalfOpen", got)
	}
	if !b.Allow() {
		t.Error("Allow() should return true in HalfOpen")
	}
}

// TestRecoverFromHalfOpen verifies that RecoverThreshold successes close the circuit.
func TestRecoverFromHalfOpen(t *testing.T) {
	b := circuitbreaker.New(circuitbreaker.Config{
		Name:             "p",
		FailThreshold:    1,
		RecoverThreshold: 2,
		OpenDuration:     1 * time.Millisecond,
	}, nil)
	b.Record(false) // trip to Open
	time.Sleep(5 * time.Millisecond)
	_ = b.State() // trigger transition to HalfOpen

	b.Record(true) // 1st success in HalfOpen; not enough
	if b.State() != circuitbreaker.StateHalfOpen {
		t.Errorf("after 1 success = %v; want HalfOpen", b.State())
	}
	b.Record(true) // 2nd success closes
	if b.State() != circuitbreaker.StateClosed {
		t.Errorf("after 2 successes = %v; want Closed", b.State())
	}
}

// TestFailureInHalfOpenReopens verifies a failure in HalfOpen returns to Open.
func TestFailureInHalfOpenReopens(t *testing.T) {
	b := circuitbreaker.New(circuitbreaker.Config{
		Name:             "p",
		FailThreshold:    1,
		RecoverThreshold: 2,
		OpenDuration:     1 * time.Millisecond,
	}, nil)
	b.Record(false) // trip
	time.Sleep(5 * time.Millisecond)
	_ = b.State() // HalfOpen

	b.Record(false) // failure in HalfOpen -> Open
	if b.State() != circuitbreaker.StateOpen {
		t.Errorf("state after HalfOpen failure = %v; want Open", b.State())
	}
}

// TestEventBusPublish verifies that tripping the breaker publishes a degraded event.
func TestEventBusPublish(t *testing.T) {
	bus := eventbus.NewBus()
	b := circuitbreaker.New(circuitbreaker.Config{
		Name:          "p",
		FailThreshold: 1,
		OpenDuration:  time.Minute,
	}, bus)

	sub, unsub := bus.Subscribe(4)
	defer unsub()

	b.Record(false) // trip -> publishes TypeProviderDegraded

	select {
	case ev := <-sub:
		if ev.Type != eventbus.TypeProviderDegraded {
			t.Errorf("event type = %q; want %q", ev.Type, eventbus.TypeProviderDegraded)
		}
	default:
		t.Error("expected a provider.degraded event on trip, got none")
	}
}

// TestStats verifies the cumulative probe counters.
func TestStats(t *testing.T) {
	b := newBreaker(10, 2, time.Minute)
	for i := 0; i < 4; i++ {
		b.Record(true)
	}
	for i := 0; i < 3; i++ {
		b.Record(false)
	}
	succ, fail := b.Stats()
	if succ != 4 {
		t.Errorf("successes = %d; want 4", succ)
	}
	if fail != 3 {
		t.Errorf("failures = %d; want 3", fail)
	}
}
