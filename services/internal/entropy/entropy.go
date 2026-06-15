// Package entropy provides an early-boot entropy readiness check for NuraOS services.
//
// On Linux 3.17+, Go's crypto/rand.Read uses getrandom(2) with flag 0, which
// blocks until the kernel CSPRNG is initialized (seeded with at least 256 bits
// of entropy). WaitReady wraps this with a timeout so callers can log a warning
// and continue rather than hanging indefinitely if virtio-rng is absent.
package entropy

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// WaitReady blocks until the OS CSPRNG is seeded or timeout elapses.
// Returns true if the CSPRNG is confirmed ready, false if the timeout was hit.
// A timeout does not indicate a fatal error -- the caller should log a warning
// and continue; Go's crypto operations will still block internally until seeded.
func WaitReady(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ch := make(chan bool, 1)
	go func() {
		buf := [32]byte{}
		_, err := rand.Read(buf[:])
		ch <- err == nil
	}()
	select {
	case ok := <-ch:
		return ok
	case <-ctx.Done():
		return false
	}
}

// AvailBits reads the current estimate of available entropy bits from
// /proc/sys/kernel/random/entropy_avail. Returns 0 on read error.
func AvailBits() int {
	raw, err := os.ReadFile("/proc/sys/kernel/random/entropy_avail")
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0
	}
	return n
}

// Check reads entropy readiness information and returns a human-readable
// summary suitable for logging. Never returns an error.
func Check() string {
	bits := AvailBits()
	if bits == 0 {
		return "entropy: /proc/sys/kernel/random/entropy_avail not readable (non-Linux?)"
	}
	var status string
	switch {
	case bits >= 256:
		status = "ready"
	case bits >= 64:
		status = "low"
	default:
		status = "very low"
	}
	return fmt.Sprintf("entropy: %d bits available (%s)", bits, status)
}
