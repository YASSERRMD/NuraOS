package healthprobe_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/healthprobe"
)

func discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 10}))
}

// TestProbeSuccess verifies a 200 response produces a successful Result.
func TestProbeSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	out := make(chan healthprobe.Result, 1)
	p := healthprobe.New(healthprobe.Config{
		Name:     "test",
		URL:      srv.URL,
		Interval: time.Hour, // no repeat
	}, out, discard())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	select {
	case r := <-out:
		if !r.Success {
			t.Errorf("expected success, got failure: %v", r.Err)
		}
		if r.Status != http.StatusOK {
			t.Errorf("status = %d; want 200", r.Status)
		}
		if r.Name != "test" {
			t.Errorf("name = %q; want test", r.Name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for probe result")
	}
}

// TestProbeFailureOn5xx verifies that a 500 response produces a failed Result.
func TestProbeFailureOn5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	out := make(chan healthprobe.Result, 1)
	p := healthprobe.New(healthprobe.Config{
		Name:     "svc",
		URL:      srv.URL,
		Interval: time.Hour,
	}, out, discard())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	select {
	case r := <-out:
		if r.Success {
			t.Errorf("expected failure for 500 response, got success")
		}
		if r.Status != 500 {
			t.Errorf("status = %d; want 500", r.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for probe result")
	}
}

// TestProbeFailureUnreachable verifies an unreachable endpoint produces a failed Result.
func TestProbeFailureUnreachable(t *testing.T) {
	out := make(chan healthprobe.Result, 1)
	p := healthprobe.New(healthprobe.Config{
		Name:     "unreachable",
		URL:      "http://127.0.0.1:1", // nothing listening on port 1
		Interval: time.Hour,
		Timeout:  200 * time.Millisecond,
	}, out, discard())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	select {
	case r := <-out:
		if r.Success {
			t.Error("expected failure for unreachable endpoint, got success")
		}
		if r.Err == nil {
			t.Error("expected non-nil error for unreachable endpoint")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for probe result")
	}
}

// TestRunExitsOnContextCancel verifies Run returns when context is cancelled.
func TestRunExitsOnContextCancel(t *testing.T) {
	out := make(chan healthprobe.Result, 4)
	p := healthprobe.New(healthprobe.Config{
		Name:     "x",
		URL:      "http://127.0.0.1:1",
		Interval: time.Hour,
		Timeout:  50 * time.Millisecond,
	}, out, discard())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancel")
	}
}
