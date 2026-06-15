package timesync

import (
	"sync"
	"sync/atomic"
	"time"
)

// stepThreshold is the minimum discrepancy between wall and monotonic elapsed
// time that triggers a ClockStep event.
const stepThreshold = time.Second

// MonoTime pairs a wall clock reading with a monotonically increasing
// per-process sequence number. The sequence starts at 1 and never resets.
// Use Seq for causal ordering when the wall clock cannot be trusted.
type MonoTime struct {
	Wall time.Time
	Seq  uint64
}

// ClockStep describes a discontinuity detected between the wall clock and the
// monotonic clock. A positive Delta means the wall clock jumped forward
// (e.g. NTP step-up); negative means it jumped backward.
type ClockStep struct {
	Before time.Time
	After  time.Time
	Delta  time.Duration // wallElapsed - monoElapsed
	Seq    uint64
}

// Clock provides thread-safe timestamps with monotonic sequencing and
// automatic clock-step detection. Obtain one via NewClock.
type Clock struct {
	seq    atomic.Uint64
	mu     sync.Mutex
	last   time.Time
	stepCh chan ClockStep
}

// NewClock returns an initialised Clock ready for use.
func NewClock() *Clock {
	c := &Clock{stepCh: make(chan ClockStep, 16)}
	c.last = time.Now()
	return c
}

// Now returns the current time tagged with a monotonically increasing
// sequence number. It detects wall-clock steps and emits them on StepEvents.
func (c *Clock) Now() MonoTime {
	now := time.Now()
	seq := c.seq.Add(1)

	c.mu.Lock()
	last := c.last
	c.last = now
	c.mu.Unlock()

	// Detect step: compare monotonic elapsed (Go Sub uses mono when available)
	// with wall elapsed (Round(0) strips the mono component).
	if seq > 1 {
		monoElapsed := now.Sub(last)
		wallElapsed := now.Round(0).Sub(last.Round(0))
		delta := wallElapsed - monoElapsed
		if delta < 0 {
			delta = -delta
		}
		if delta > stepThreshold {
			step := ClockStep{
				Before: last.Round(0),
				After:  now.Round(0),
				Delta:  wallElapsed - monoElapsed,
				Seq:    seq,
			}
			select {
			case c.stepCh <- step:
			default: // channel full: discard; next step will emit
			}
		}
	}

	return MonoTime{Wall: now.Round(0), Seq: seq}
}

// StepEvents returns the read-only channel on which ClockStep events are emitted.
func (c *Clock) StepEvents() <-chan ClockStep { return c.stepCh }
