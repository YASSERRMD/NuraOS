// Package providerhealth ties together health probes and circuit breakers for
// each configured inference provider.
//
// The Manager runs one Prober per provider in a goroutine and feeds probe
// results into that provider's Breaker. Routing code calls ShouldFallback to
// determine whether a provider is currently reachable. The full state snapshot
// is exposed for /status and /metrics surfaces.
package providerhealth

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/circuitbreaker"
	"github.com/yasserrmd/nuraos/services/internal/eventbus"
	"github.com/yasserrmd/nuraos/services/internal/healthprobe"
)

// ProviderConfig describes health probe and circuit breaker parameters for one
// provider.
type ProviderConfig struct {
	// Name is the provider identifier (e.g. "anthropic", "openai", "local").
	Name string
	// ProbeURL is the HTTP endpoint to check for reachability.
	ProbeURL string
	// Interval between health probes. Default: 30 s.
	Interval time.Duration
	// Timeout for a single probe request. Default: 5 s.
	Timeout time.Duration
	// FailThreshold is consecutive failures before tripping the breaker.
	FailThreshold int
	// RecoverThreshold is consecutive successes in HalfOpen before closing.
	RecoverThreshold int
	// OpenDuration is how long the breaker stays Open before trying HalfOpen.
	OpenDuration time.Duration
}

// Status is a point-in-time snapshot of one provider's health and circuit state.
type Status struct {
	Name           string `json:"name"`
	CircuitState   string `json:"circuit_state"`
	Reachable      bool   `json:"reachable"`
	ProbeSuccesses int64  `json:"probe_successes_total"`
	ProbeFailures  int64  `json:"probe_failures_total"`
	ShouldFallback bool   `json:"should_fallback"`
}

type entry struct {
	cfg     ProviderConfig
	breaker *circuitbreaker.Breaker
}

// Manager runs health probes and circuit breakers for a set of providers.
// The zero value is usable as a no-op manager with no configured providers.
type Manager struct {
	bus *eventbus.Bus
	log *slog.Logger

	mu      sync.RWMutex
	entries []*entry
}

// New creates a Manager. bus may be nil; log must not be nil.
func New(bus *eventbus.Bus, log *slog.Logger) *Manager {
	return &Manager{bus: bus, log: log}
}

// Add registers a provider. It must be called before Run.
func (m *Manager) Add(cfg ProviderConfig) {
	b := circuitbreaker.New(circuitbreaker.Config{
		Name:             cfg.Name,
		FailThreshold:    cfg.FailThreshold,
		RecoverThreshold: cfg.RecoverThreshold,
		OpenDuration:     cfg.OpenDuration,
	}, m.bus)

	m.mu.Lock()
	m.entries = append(m.entries, &entry{cfg: cfg, breaker: b})
	m.mu.Unlock()
}

// Run starts health probes for all registered providers. It blocks until ctx
// is cancelled; safe to call in a goroutine.
func (m *Manager) Run(ctx context.Context) {
	m.mu.RLock()
	entries := make([]*entry, len(m.entries))
	copy(entries, m.entries)
	m.mu.RUnlock()

	if len(entries) == 0 {
		<-ctx.Done()
		return
	}

	results := make(chan healthprobe.Result, len(entries)*4)

	byName := make(map[string]*entry, len(entries))
	for _, e := range entries {
		byName[e.cfg.Name] = e
		p := healthprobe.New(healthprobe.Config{
			Name:     e.cfg.Name,
			URL:      e.cfg.ProbeURL,
			Interval: e.cfg.Interval,
			Timeout:  e.cfg.Timeout,
		}, results, m.log)
		go p.Run(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case r := <-results:
			e, ok := byName[r.Name]
			if !ok {
				continue
			}
			e.breaker.Record(r.Success)
			if !r.Success {
				m.log.Warn("provider probe failed",
					"provider", r.Name,
					"circuit", e.breaker.State(),
					"err", r.Err,
				)
			}
		}
	}
}

// ShouldFallback reports whether routing should avoid using this provider and
// fall back to local inference. Returns false for unknown providers.
func (m *Manager) ShouldFallback(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.entries {
		if e.cfg.Name == name {
			return !e.breaker.Allow()
		}
	}
	return false
}

// Snapshot returns the current health state for all registered providers.
func (m *Manager) Snapshot() []Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]Status, 0, len(m.entries))
	for _, e := range m.entries {
		st := e.breaker.State()
		succ, fail := e.breaker.Stats()
		out = append(out, Status{
			Name:           e.cfg.Name,
			CircuitState:   st.String(),
			Reachable:      st != circuitbreaker.StateOpen,
			ProbeSuccesses: succ,
			ProbeFailures:  fail,
			ShouldFallback: !e.breaker.Allow(),
		})
	}
	return out
}
