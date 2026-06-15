package sockact_test

import (
	"net"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/sockact"
)

func TestNewHolderBindsSocket(t *testing.T) {
	h, err := sockact.NewHolder("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	if h.Network() != "tcp" {
		t.Errorf("Network() = %q, want tcp", h.Network())
	}
	if h.Address() == "" {
		t.Error("Address() is empty")
	}
}

func TestWaitFirstConnection(t *testing.T) {
	h, err := sockact.NewHolder("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	// Dial the holder's actual bound address from a goroutine.
	addr := h.Address()
	stop := make(chan struct{})

	errCh := make(chan error, 1)
	go func() {
		errCh <- h.WaitFirstConnection(stop)
	}()

	// Give the goroutine a moment to start.
	time.Sleep(10 * time.Millisecond)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("WaitFirstConnection returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitFirstConnection timed out")
	}

	if h.ConnectionCount() != 1 {
		t.Errorf("ConnectionCount() = %d, want 1", h.ConnectionCount())
	}
}

func TestWaitFirstConnectionCancelled(t *testing.T) {
	h, err := sockact.NewHolder("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	stop := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- h.WaitFirstConnection(stop)
	}()

	time.Sleep(20 * time.Millisecond)
	close(stop)

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected non-nil error when cancelled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitFirstConnection did not return after cancel")
	}
}

func TestIdleFor(t *testing.T) {
	h, err := sockact.NewHolder("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	h.TouchActivity()
	time.Sleep(50 * time.Millisecond)
	idle := h.IdleFor()
	if idle < 40*time.Millisecond {
		t.Errorf("IdleFor() = %v, expected >= 40ms", idle)
	}
}
