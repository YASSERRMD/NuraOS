package journal

import (
	"sync"
	"time"
)

// FloodLimiter enforces a per-service burst cap over a sliding one-second
// window. Records exceeding the limit are silently dropped at the Writer level.
type FloodLimiter struct {
	maxPerSec int
	mu        sync.Mutex
	buckets   map[string]*floodBucket
}

type floodBucket struct {
	count int
	reset time.Time
}

// NewFloodLimiter creates a FloodLimiter allowing up to maxPerSec records per
// service per second. Values <= 0 default to 200.
func NewFloodLimiter(maxPerSec int) *FloodLimiter {
	if maxPerSec <= 0 {
		maxPerSec = 200
	}
	return &FloodLimiter{
		maxPerSec: maxPerSec,
		buckets:   make(map[string]*floodBucket),
	}
}

// Allow returns true if a record from service should be written.
// It returns false once the service has exceeded maxPerSec in the current
// one-second window.
func (f *FloodLimiter) Allow(service string) bool {
	now := time.Now()
	f.mu.Lock()
	defer f.mu.Unlock()

	b, ok := f.buckets[service]
	if !ok {
		b = &floodBucket{reset: now.Add(time.Second)}
		f.buckets[service] = b
	}
	if now.After(b.reset) {
		b.count = 0
		b.reset = now.Add(time.Second)
	}
	b.count++
	return b.count <= f.maxPerSec
}
