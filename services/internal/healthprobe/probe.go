// Package healthprobe fires periodic HTTP health checks against a provider
// endpoint and reports results on a channel. It is intentionally decoupled
// from circuit-breaker logic so the two can be composed freely.
package healthprobe

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

const (
	defaultInterval = 30 * time.Second
	defaultTimeout  = 5 * time.Second
)

// Config describes a single provider's probe parameters.
type Config struct {
	// Name is the human-readable provider label (used in Result and logs).
	Name string
	// URL is the HTTP endpoint to probe. A 2xx response is a success.
	URL string
	// Interval between probes. Default: 30 s.
	Interval time.Duration
	// Timeout for a single probe HTTP request. Default: 5 s.
	Timeout time.Duration
}

// Result is the outcome of one probe attempt.
type Result struct {
	Name    string
	Success bool
	Latency time.Duration
	Status  int   // HTTP status code; 0 if the request could not be completed
	Err     error // non-nil when the request itself failed
}

// Prober fires periodic health probes for one provider. Probe results are sent
// to the out channel; if the channel is full the result is dropped silently so
// the prober never blocks. The caller is responsible for draining the channel.
type Prober struct {
	cfg    Config
	client *http.Client
	log    *slog.Logger
	out    chan<- Result
}

// New creates a Prober. out is the result channel; log may be a discard logger
// but must not be nil.
func New(cfg Config, out chan<- Result, log *slog.Logger) *Prober {
	to := cfg.Timeout
	if to <= 0 {
		to = defaultTimeout
	}
	return &Prober{
		cfg:    cfg,
		client: &http.Client{Timeout: to},
		log:    log,
		out:    out,
	}
}

// Run fires an immediate probe then repeats on cfg.Interval until ctx is
// cancelled. Safe to call in a goroutine.
func (p *Prober) Run(ctx context.Context) {
	p.fireOnce(ctx)

	interval := p.cfg.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.fireOnce(ctx)
		}
	}
}

func (p *Prober) fireOnce(ctx context.Context) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.URL, nil)
	if err != nil {
		p.send(Result{Name: p.cfg.Name, Success: false, Err: err})
		return
	}

	resp, err := p.client.Do(req)
	latency := time.Since(start)
	if err != nil {
		p.log.Debug("health probe failed", "provider", p.cfg.Name, "err", err, "latency", latency)
		p.send(Result{Name: p.cfg.Name, Success: false, Latency: latency, Err: err})
		return
	}
	resp.Body.Close()

	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	p.log.Debug("health probe", "provider", p.cfg.Name, "status", resp.StatusCode, "latency", latency)
	p.send(Result{Name: p.cfg.Name, Success: success, Latency: latency, Status: resp.StatusCode})
}

func (p *Prober) send(r Result) {
	select {
	case p.out <- r:
	default:
	}
}
