package entropy_test

import (
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/entropy"
)

func TestWaitReadySucceeds(t *testing.T) {
	// On any OS with a seeded CSPRNG (macOS, Linux post-boot), this should
	// return true almost immediately.
	if !entropy.WaitReady(5 * time.Second) {
		t.Fatal("WaitReady returned false: CSPRNG did not become ready within 5s")
	}
}

func TestWaitReadyTimeout(t *testing.T) {
	// A very short timeout may or may not fire depending on OS scheduling, but
	// it must not panic or block forever.
	_ = entropy.WaitReady(1 * time.Nanosecond)
}

func TestCheckReturnsString(t *testing.T) {
	s := entropy.Check()
	if s == "" {
		t.Fatal("Check returned empty string")
	}
}

func TestAvailBitsNonNegative(t *testing.T) {
	n := entropy.AvailBits()
	if n < 0 {
		t.Fatalf("AvailBits returned %d; want >= 0", n)
	}
}
