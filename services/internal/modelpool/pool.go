// Package modelpool manages the on-device model lifecycle for llama-server.
//
// State machine:
//
//	Unloaded -> Loading -> Loaded -> Unloading -> Unloaded
//
// Lazy load: the first request to Acquire() when state is Unloaded triggers a
// Start command via the manager control socket. Subsequent callers wait on the
// condition variable until the state transitions to Loaded.
//
// Idle unload: a background goroutine (Run) checks the time since the last
// Release() and sends a Stop command when it exceeds IdleTimeout. If
// WarmPool=true and the inference cgroup has enough free memory (>MemoryMargin),
// the unload is skipped.
//
// First-request warm-up coordination: socket activation ensures the gateway's
// inbound socket is already accepting connections. Pool.Acquire blocks until
// the llama-server readiness probe succeeds (probed via the manager status).
package modelpool

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/cgroup"
	"github.com/yasserrmd/nuraos/services/internal/ctlsock"
	"github.com/yasserrmd/nuraos/services/internal/eventbus"
)

// ModelState is the current lifecycle state of the inference model.
type ModelState int

const (
	StateUnloaded  ModelState = iota
	StateLoading              // Start sent; waiting for service readiness
	StateLoaded               // Service running and healthy
	StateUnloading            // Stop sent; waiting for service to exit
)

func (s ModelState) String() string {
	switch s {
	case StateUnloaded:
		return "unloaded"
	case StateLoading:
		return "loading"
	case StateLoaded:
		return "loaded"
	case StateUnloading:
		return "unloading"
	default:
		return "unknown"
	}
}

// Config controls the model lifecycle behaviour.
type Config struct {
	// ServiceName is the managed service (default: "llama-server").
	ServiceName string
	// CtlSocket is the manager control socket (default: ctlsock.SocketPath).
	CtlSocket string
	// IdleTimeout is how long to wait after the last Release before unloading.
	// 0 means never auto-unload (always keep loaded once started).
	IdleTimeout time.Duration
	// WarmPool keeps the model loaded when memory allows even after idle timeout.
	WarmPool bool
	// MemoryMargin is the minimum free bytes in the inference cgroup required to
	// keep the warm pool active. Only used when WarmPool=true.
	MemoryMargin uint64
	// ReadinessTimeout is how long to wait for the model to become ready.
	// Default: 120 s.
	ReadinessTimeout time.Duration
}

func (c *Config) serviceName() string {
	if c.ServiceName != "" {
		return c.ServiceName
	}
	return "llama-server"
}

func (c *Config) ctlSocket() string {
	if c.CtlSocket != "" {
		return c.CtlSocket
	}
	return ctlsock.SocketPath
}

func (c *Config) readinessTimeout() time.Duration {
	if c.ReadinessTimeout > 0 {
		return c.ReadinessTimeout
	}
	return 120 * time.Second
}

// Pool manages the on-device model lifecycle.
type Pool struct {
	cfg     Config
	log     *slog.Logger
	bus     *eventbus.Bus
	cgMgr   *cgroup.Manager

	mu          sync.Mutex
	cond        *sync.Cond
	state       ModelState
	lastRelease time.Time
}

// New creates a Pool with the given configuration.
func New(cfg Config, bus *eventbus.Bus, log *slog.Logger) *Pool {
	p := &Pool{
		cfg:         cfg,
		bus:         bus,
		log:         log,
		cgMgr:       cgroup.NewManager(),
		lastRelease: time.Now(),
	}
	p.cond = sync.NewCond(&p.mu)
	return p
}

// State returns the current model state.
func (p *Pool) State() ModelState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// Acquire ensures the model is loaded and returns when it is ready.
// The caller must call Release() when inference is complete.
// Returns an error if the model cannot be loaded within ReadinessTimeout.
func (p *Pool) Acquire(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch p.state {
	case StateLoaded:
		return nil

	case StateLoading:
		// Another goroutine already triggered the load; wait for it.
		return p.waitReady(ctx)

	case StateUnloading:
		// Wait for current unload to finish, then trigger a new load.
		p.waitState(StateUnloaded)
		fallthrough

	case StateUnloaded:
		if err := p.sendStart(); err != nil {
			return fmt.Errorf("model load failed: %w", err)
		}
		p.setState(StateLoading)
		return p.waitReady(ctx)
	}
	return nil
}

// Release records that an inference is complete. It does NOT unload the model;
// that is done by the idle-timeout loop in Run.
func (p *Pool) Release() {
	p.mu.Lock()
	p.lastRelease = time.Now()
	p.mu.Unlock()
}

// Run is the background lifecycle loop. It polls for idle timeout and manages
// the warm pool. Call in a goroutine; exits when ctx is cancelled.
func (p *Pool) Run(ctx context.Context) {
	if p.cfg.IdleTimeout == 0 {
		// No auto-unload; just wait for context cancellation.
		<-ctx.Done()
		return
	}

	ticker := time.NewTicker(p.cfg.IdleTimeout / 4)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.checkIdle(ctx)
		}
	}
}

// NotifyLoaded should be called (e.g., by the manager after readiness probe)
// to signal that the model is ready.
func (p *Pool) NotifyLoaded() {
	p.mu.Lock()
	if p.state == StateLoading {
		p.setState(StateLoaded)
		p.cond.Broadcast()
	}
	p.mu.Unlock()
}

// NotifyUnloaded should be called after the service has stopped.
func (p *Pool) NotifyUnloaded() {
	p.mu.Lock()
	p.setState(StateUnloaded)
	p.cond.Broadcast()
	p.mu.Unlock()
}

// --- internal ---

func (p *Pool) checkIdle(_ context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state != StateLoaded {
		return
	}

	idle := time.Since(p.lastRelease)
	if idle < p.cfg.IdleTimeout {
		return
	}

	// Check warm pool: if memory allows, skip unload.
	if p.cfg.WarmPool {
		stats := p.cgMgr.ReadStats(p.cfg.serviceName())
		if stats != nil && stats.MemoryMax > 0 {
			free := stats.MemoryMax - stats.MemoryCurrent
			if free >= p.cfg.MemoryMargin {
				p.log.Debug("model pool: warm pool active; skipping idle unload",
					"idle", idle, "free_mib", free/(1024*1024))
				return
			}
		}
	}

	p.log.Info("model pool: idle timeout exceeded; unloading model",
		"idle", idle, "service", p.cfg.serviceName())
	_ = p.sendStop()
	p.setState(StateUnloading)
}

func (p *Pool) waitReady(ctx context.Context) error {
	deadline := time.Now().Add(p.cfg.readinessTimeout())
	for p.state == StateLoading {
		if time.Now().After(deadline) {
			return fmt.Errorf("model load timed out after %s", p.cfg.readinessTimeout())
		}
		// Use a goroutine to respect ctx cancellation alongside the cond wait.
		waitDone := make(chan struct{})
		go func() {
			p.cond.Wait()
			close(waitDone)
		}()
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			p.mu.Lock()
			return ctx.Err()
		case <-waitDone:
			p.mu.Lock()
		}
	}
	if p.state != StateLoaded {
		return fmt.Errorf("model failed to load (state: %s)", p.state)
	}
	return nil
}

func (p *Pool) waitState(target ModelState) {
	for p.state != target {
		p.cond.Wait()
	}
}

func (p *Pool) setState(s ModelState) {
	if p.state == s {
		return
	}
	prev := p.state
	p.state = s
	p.log.Info("model pool: state changed", "from", prev, "to", s, "service", p.cfg.serviceName())
	if p.bus != nil {
		p.bus.Publish(eventbus.NewEvent(eventbus.TypeModelStateChanged, "modelpool", map[string]any{
			"service":  p.cfg.serviceName(),
			"previous": prev.String(),
			"current":  s.String(),
		}))
	}
}

func (p *Pool) sendStart() error {
	c := ctlsock.NewClient(p.cfg.ctlSocket())
	resp, err := c.Send(ctlsock.Request{Command: ctlsock.CmdStart, Service: p.cfg.serviceName()})
	if err != nil {
		return fmt.Errorf("start command: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("start failed: %s", resp.Error)
	}
	return nil
}

func (p *Pool) sendStop() error {
	c := ctlsock.NewClient(p.cfg.ctlSocket())
	resp, err := c.Send(ctlsock.Request{Command: ctlsock.CmdStop, Service: p.cfg.serviceName()})
	if err != nil {
		return fmt.Errorf("stop command: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("stop failed: %s", resp.Error)
	}
	return nil
}
