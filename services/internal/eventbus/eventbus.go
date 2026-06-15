// Package eventbus provides a lightweight in-process pub/sub broker for
// NuraOS system events. Publishers call Publish; subscribers receive events
// on a buffered channel. A Unix-socket Server allows out-of-process
// subscribers (e.g. nuractl events).
//
// Backpressure: each subscriber has a bounded queue (default 256). If the
// queue is full when a publisher calls Publish, the event is dropped for that
// subscriber only. The publisher never blocks. Slow subscribers cannot stall
// system components.
package eventbus

import (
	"sync"
	"time"
)

// System event type constants.
const (
	TypeServiceStarted  = "service.started"
	TypeServiceStopped  = "service.stopped"
	TypeServiceFailed   = "service.failed"
	TypeDiskWarn        = "disk.warn"
	TypeDiskCritical    = "disk.critical"
	TypeOOMKilled       = "oom.killed"
	TypeClockStep       = "clock.step"
	TypeProviderHealthy = "provider.healthy"
	TypeProviderDegraded = "provider.degraded"
)

// DefaultSubBufSize is the number of events buffered per subscriber before
// events are dropped for that subscriber.
const DefaultSubBufSize = 256

// Event is a system event published on the bus.
type Event struct {
	Type    string `json:"type"`
	Source  string `json:"source"`
	At      string `json:"at"`
	Payload any    `json:"payload,omitempty"`
}

// NewEvent creates an Event with the current UTC timestamp.
func NewEvent(typ, source string, payload any) Event {
	return Event{
		Type:    typ,
		Source:  source,
		At:      time.Now().UTC().Format(time.RFC3339),
		Payload: payload,
	}
}

type subscriber struct {
	ch chan Event
}

// Bus is an in-process pub/sub broker. The zero value is not usable; use NewBus.
type Bus struct {
	mu   sync.Mutex
	subs []*subscriber
}

// NewBus creates a new Bus.
func NewBus() *Bus { return &Bus{} }

// Publish broadcasts ev to all current subscribers. If a subscriber's queue
// is full the event is silently dropped for that subscriber only.
func (b *Bus) Publish(ev Event) {
	if ev.At == "" {
		ev.At = time.Now().UTC().Format(time.RFC3339)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.subs {
		select {
		case s.ch <- ev:
		default:
		}
	}
}

// Subscribe registers a new subscriber and returns its event channel and a
// cancel function. The caller must call cancel when done; cancel closes the
// channel and removes the subscriber from the bus. bufSize sets the channel
// capacity; 0 uses DefaultSubBufSize.
func (b *Bus) Subscribe(bufSize int) (<-chan Event, func()) {
	if bufSize <= 0 {
		bufSize = DefaultSubBufSize
	}
	s := &subscriber{ch: make(chan Event, bufSize)}
	b.mu.Lock()
	b.subs = append(b.subs, s)
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		for i, sub := range b.subs {
			if sub == s {
				b.subs = append(b.subs[:i], b.subs[i+1:]...)
				break
			}
		}
		b.mu.Unlock()
		close(s.ch)
	}
	return s.ch, cancel
}
