// Package circuitbreaker implements a three-state circuit breaker for provider
// health management.
//
// States:
//
//	Closed   -> normal; requests are allowed; consecutive failures trip to Open
//	Open     -> tripped; requests are blocked; after OpenDuration tries HalfOpen
//	HalfOpen -> recovery; next probe success count determines Close or re-Open
//
// Each state transition publishes a provider.healthy or provider.degraded event
// on the event bus so operators can react without polling.
package circuitbreaker

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/eventbus"
)

// State is the current circuit breaker state.
type State int

const (
	// StateClosed is the normal state; requests are allowed.
	StateClosed State = iota
	// StateOpen is the tripped state; requests are blocked until OpenDuration elapses.
	StateOpen
	// StateHalfOpen allows recovery probes; closes on enough successes.
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Config controls the circuit breaker behaviour for a single provider.
type Config struct {
	// Name is the provider name, used in log and event payloads.
	Name string
	// FailThreshold is the number of consecutive probe failures before tripping.
	// Default: 3.
	FailThreshold int
	// RecoverThreshold is the number of consecutive probe successes in HalfOpen
	// before closing the circuit.
	// Default: 2.
	RecoverThreshold int
	// OpenDuration is how long the circuit stays Open before trying HalfOpen.
	// Default: 30 s.
	OpenDuration time.Duration
}

func (c Config) failThreshold() int {
	if c.FailThreshold > 0 {
		return c.FailThreshold
	}
	return 3
}

func (c Config) recoverThreshold() int {
	if c.RecoverThreshold > 0 {
		return c.RecoverThreshold
	}
	return 2
}

func (c Config) openDuration() time.Duration {
	if c.OpenDuration > 0 {
		return c.OpenDuration
	}
	return 30 * time.Second
}

// Breaker is a three-state circuit breaker. The zero value is not usable; use New.
type Breaker struct {
	cfg Config
	bus *eventbus.Bus

	mu           sync.Mutex
	state        State
	failCount    int
	successCount int
	openedAt     time.Time

	probeSuccesses atomic.Int64
	probeFailures  atomic.Int64
}

// New creates a Breaker. bus may be nil (events are skipped).
func New(cfg Config, bus *eventbus.Bus) *Breaker {
	return &Breaker{cfg: cfg, bus: bus}
}

// State returns the current circuit breaker state.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.maybeTransitionToHalfOpen()
	return b.state
}

// Allow reports whether a request to the provider should be attempted.
// Returns false when the circuit is Open and the cooldown has not elapsed.
// Transitions Open -> HalfOpen automatically once OpenDuration has passed.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.maybeTransitionToHalfOpen()
	return b.state != StateOpen
}

// Record records the outcome of a health probe. Record is the only way to
// advance the state machine; it must be called for every probe result.
func (b *Breaker) Record(success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if success {
		b.probeSuccesses.Add(1)
		switch b.state {
		case StateClosed:
			b.failCount = 0
		case StateHalfOpen:
			b.successCount++
			if b.successCount >= b.cfg.recoverThreshold() {
				b.setState(StateClosed)
				b.failCount = 0
				b.successCount = 0
			}
		}
	} else {
		b.probeFailures.Add(1)
		switch b.state {
		case StateClosed:
			b.failCount++
			if b.failCount >= b.cfg.failThreshold() {
				b.openedAt = time.Now()
				b.setState(StateOpen)
				b.failCount = 0
			}
		case StateHalfOpen:
			b.openedAt = time.Now()
			b.setState(StateOpen)
			b.successCount = 0
		}
	}
}

// Stats returns the cumulative probe success and failure counts since creation.
func (b *Breaker) Stats() (successes, failures int64) {
	return b.probeSuccesses.Load(), b.probeFailures.Load()
}

// maybeTransitionToHalfOpen transitions Open -> HalfOpen if OpenDuration has elapsed.
// Caller must hold b.mu.
func (b *Breaker) maybeTransitionToHalfOpen() {
	if b.state == StateOpen && time.Since(b.openedAt) >= b.cfg.openDuration() {
		b.setState(StateHalfOpen)
		b.successCount = 0
	}
}

// setState transitions to s and publishes a bus event. Caller must hold b.mu.
func (b *Breaker) setState(s State) {
	if b.state == s {
		return
	}
	prev := b.state
	b.state = s

	if b.bus == nil {
		return
	}
	evType := eventbus.TypeProviderHealthy
	if s == StateOpen {
		evType = eventbus.TypeProviderDegraded
	}
	b.bus.Publish(eventbus.NewEvent(evType, "circuitbreaker", map[string]any{
		"provider": b.cfg.Name,
		"previous": prev.String(),
		"current":  s.String(),
	}))
}
