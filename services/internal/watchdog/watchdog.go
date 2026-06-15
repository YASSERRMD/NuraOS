// Package watchdog manages a hardware /dev/watchdog and a software escalation
// ladder so the system reboots automatically when NuraOS hangs.
//
// Architecture:
//
//   - A Petter goroutine opens /dev/watchdog and writes a keep-alive byte
//     every PetInterval. If it does not pet within HardwareTimeout the kernel
//     triggers a hard reset.
//
//   - A Supervisor goroutine calls a user-supplied HealthFunc on every
//     SoftwareInterval. If HealthFunc returns false SoftTries consecutive times
//     the Supervisor escalates: it stops petting the hardware watchdog so a
//     hard reset follows within HardwareTimeout.
//
// The hardware watchdog is opened with the standard Linux /dev/watchdog
// interface (write any byte to pet; close WITHOUT writing "V" lets it expire).
// On QEMU, use `-device i6300esb,id=wdog0` to expose this device.
package watchdog

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"
)

const (
	// DevPath is the standard Linux hardware watchdog device.
	DevPath = "/dev/watchdog"

	// DefaultHardwareTimeout is the kernel watchdog expiry window.
	// The kernel must be informed of this via WDIOC_SETTIMEOUT ioctl; absent
	// that call the kernel uses its compiled-in default (typically 60 s).
	// We keep petting every PetInterval which is well inside this window.
	DefaultHardwareTimeout = 30 * time.Second

	// DefaultPetInterval is how often the petter writes to /dev/watchdog.
	DefaultPetInterval = 10 * time.Second

	// DefaultSoftwareInterval is how often HealthFunc is called.
	DefaultSoftwareInterval = 5 * time.Second

	// DefaultSoftTries is the number of consecutive health failures before
	// the supervisor escalates (stops petting the hardware watchdog).
	DefaultSoftTries = 3
)

// Config holds tunable parameters for the watchdog subsystem.
type Config struct {
	// DevPath overrides the hardware watchdog device path (default /dev/watchdog).
	DevPath string
	// PetInterval controls how often the hardware watchdog is pet.
	PetInterval time.Duration
	// SoftwareInterval controls how often HealthFunc is polled.
	SoftwareInterval time.Duration
	// SoftTries is the number of consecutive failures before escalation.
	SoftTries int
	// HealthFunc is called by the Supervisor to check system health.
	// Return true = healthy; false = unhealthy. Must not block longer than
	// SoftwareInterval.
	HealthFunc func() bool
	// Log is used for diagnostic messages. Nil disables logging.
	Log *slog.Logger
}

func (c *Config) devPath() string {
	if c.DevPath != "" {
		return c.DevPath
	}
	return DevPath
}

func (c *Config) petInterval() time.Duration {
	if c.PetInterval > 0 {
		return c.PetInterval
	}
	return DefaultPetInterval
}

func (c *Config) softInterval() time.Duration {
	if c.SoftwareInterval > 0 {
		return c.SoftwareInterval
	}
	return DefaultSoftwareInterval
}

func (c *Config) softTries() int {
	if c.SoftTries > 0 {
		return c.SoftTries
	}
	return DefaultSoftTries
}

// Watchdog manages the hardware watchdog device and the software escalation
// ladder. The zero value is not useful; use New.
type Watchdog struct {
	cfg  Config
	mu   sync.Mutex
	fd   *os.File   // nil when hardware watchdog is not open or is disabled
	stop chan struct{}
	pet  chan struct{} // closed by supervisor to stop petting
	wg   sync.WaitGroup
}

// New creates a Watchdog with the given configuration.
func New(cfg Config) *Watchdog {
	return &Watchdog{
		cfg:  cfg,
		stop: make(chan struct{}),
		pet:  make(chan struct{}),
	}
}

// Start opens the hardware watchdog device and begins the petter and
// supervisor goroutines. Returns an error if the device cannot be opened;
// callers may treat a missing /dev/watchdog as non-fatal and continue without
// hardware watchdog protection.
func (w *Watchdog) Start(ctx context.Context) error {
	f, err := os.OpenFile(w.cfg.devPath(), os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.fd = f
	w.mu.Unlock()

	w.wg.Add(2)
	go w.runPetter(ctx)
	go w.runSupervisor(ctx)
	return nil
}

// StartSoftwareOnly starts only the supervisor goroutine (no hardware device).
// Use this in environments where /dev/watchdog is not available (e.g. tests).
// The supervisor will call HealthFunc and log failures but cannot trigger a
// real hardware reset.
func (w *Watchdog) StartSoftwareOnly(ctx context.Context) {
	w.wg.Add(1)
	go w.runSupervisor(ctx)
}

// Pet manually pets the watchdog once. Intended for test use.
func (w *Watchdog) Pet() {
	w.mu.Lock()
	f := w.fd
	w.mu.Unlock()
	if f != nil {
		_, _ = f.Write([]byte{0})
	}
}

// StopPetting tells the petter goroutine to cease writing to /dev/watchdog.
// After HardwareTimeout elapses the kernel will trigger a hard reset.
// This is the escalation path: the supervisor calls it when health is lost.
func (w *Watchdog) StopPetting() {
	select {
	case <-w.pet:
	default:
		close(w.pet)
	}
}

// Close cleanly shuts down the watchdog, writing the magic "V" byte to
// disarm the hardware watchdog before closing the fd. Call this on clean
// shutdown to prevent a spurious reboot.
func (w *Watchdog) Close() error {
	close(w.stop)
	w.wg.Wait()

	w.mu.Lock()
	f := w.fd
	w.fd = nil
	w.mu.Unlock()

	if f != nil {
		_, _ = f.Write([]byte("V")) // magic disarm
		return f.Close()
	}
	return nil
}

// runPetter writes to /dev/watchdog every PetInterval until StopPetting is
// called or the context is cancelled.
func (w *Watchdog) runPetter(ctx context.Context) {
	defer w.wg.Done()
	tick := time.NewTicker(w.cfg.petInterval())
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-w.pet:
			if w.cfg.Log != nil {
				w.cfg.Log.Warn("watchdog: petting stopped by supervisor; hard reset imminent")
			}
			return
		case <-tick.C:
			w.mu.Lock()
			f := w.fd
			w.mu.Unlock()
			if f != nil {
				_, _ = f.Write([]byte{0})
			}
		}
	}
}

// runSupervisor polls HealthFunc and escalates after SoftTries failures.
func (w *Watchdog) runSupervisor(ctx context.Context) {
	defer w.wg.Done()
	if w.cfg.HealthFunc == nil {
		return
	}
	tick := time.NewTicker(w.cfg.softInterval())
	defer tick.Stop()
	failures := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-tick.C:
			healthy := w.cfg.HealthFunc()
			if healthy {
				failures = 0
				continue
			}
			failures++
			if w.cfg.Log != nil {
				w.cfg.Log.Warn("watchdog: health check failed",
					"consecutive_failures", failures,
					"threshold", w.cfg.softTries(),
				)
			}
			if failures >= w.cfg.softTries() {
				if w.cfg.Log != nil {
					w.cfg.Log.Error("watchdog: escalating - stopping hardware petting",
						"failures", failures,
					)
				}
				w.StopPetting()
				return
			}
		}
	}
}
