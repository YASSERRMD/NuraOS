// Package lifecycle implements the NuraOS service state machine and manager.
//
// Each service unit transitions through:
//
//	inactive -> starting -> ready -> running -> stopping -> (inactive|failed)
//
// The notify protocol lets a Type=notify service signal readiness by writing
// "READY=1\n" to the file descriptor passed in NOTIFY_FD (similar to
// sd_notify but simpler).
package lifecycle

import (
	"fmt"
	"sync"
	"time"
)

// State represents a service's current lifecycle position.
type State int

const (
	StateInactive State = iota // not yet started or fully stopped
	StateStarting              // process launched, readiness probe pending
	StateReady                 // readiness confirmed; dependants may start
	StateRunning               // process live; no active probe
	StateStopping              // SIGTERM sent; drain period active
	StateFailed                // exited and restart policy is "no"
)

func (s State) String() string {
	switch s {
	case StateInactive:
		return "inactive"
	case StateStarting:
		return "starting"
	case StateReady:
		return "ready"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	case StateFailed:
		return "failed"
	default:
		return fmt.Sprintf("state(%d)", int(s))
	}
}

// Transition records a single state change on a service.
type Transition struct {
	From      State
	To        State
	Timestamp time.Time
	Reason    string
}

// StatusSnapshot is a safe-to-copy point-in-time view of a service's status.
type StatusSnapshot struct {
	Name        string
	State       State
	PID         int
	Restarts    int
	LastExit    int
	Since       time.Time
	Transitions []Transition
}

// serviceStatus holds the mutable, lock-protected runtime state of a service.
type serviceStatus struct {
	name        string
	state       State
	pid         int
	restarts    int
	lastExit    int
	since       time.Time
	transitions []Transition
	mu          sync.Mutex
}

func newServiceStatus(name string) *serviceStatus {
	return &serviceStatus{
		name:  name,
		state: StateInactive,
		since: time.Now(),
	}
}

// transition moves the service to the next state and records the change.
// It returns an error if the transition is not valid.
func (s *serviceStatus) transition(to State, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validTransition(s.state, to) {
		return fmt.Errorf("invalid transition %s -> %s for service %q: %s",
			s.state, to, s.name, reason)
	}
	t := Transition{
		From:      s.state,
		To:        to,
		Timestamp: time.Now(),
		Reason:    reason,
	}
	s.transitions = append(s.transitions, t)
	s.state = to
	s.since = t.Timestamp
	return nil
}

// currentState returns the current state safely.
func (s *serviceStatus) currentState() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// snapshot returns a safe, lock-free copy of the current status.
func (s *serviceStatus) snapshot() StatusSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	transitions := make([]Transition, len(s.transitions))
	copy(transitions, s.transitions)
	return StatusSnapshot{
		Name:        s.name,
		State:       s.state,
		PID:         s.pid,
		Restarts:    s.restarts,
		LastExit:    s.lastExit,
		Since:       s.since,
		Transitions: transitions,
	}
}

// validTransition returns true when moving from -> to is allowed.
func validTransition(from, to State) bool {
	allowed := map[State][]State{
		StateInactive: {StateStarting},
		StateStarting: {StateReady, StateFailed, StateStopping},
		StateReady:    {StateRunning, StateStopping, StateFailed},
		StateRunning:  {StateStopping, StateFailed, StateStarting}, // restart loops back
		StateStopping: {StateInactive, StateFailed},
		StateFailed:   {StateStarting}, // retry after crash-loop pause
	}
	for _, dst := range allowed[from] {
		if dst == to {
			return true
		}
	}
	return false
}
