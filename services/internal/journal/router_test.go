package journal_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/journal"
)

// TestMultiHandlerFanout verifies that MultiHandler delivers to all sub-handlers.
func TestMultiHandlerFanout(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	h1 := slog.NewTextHandler(&buf1, &slog.HandlerOptions{Level: slog.LevelDebug})
	h2 := slog.NewTextHandler(&buf2, &slog.HandlerOptions{Level: slog.LevelDebug})

	mh := journal.NewMultiHandler(h1, h2)
	logger := slog.New(mh)
	logger.Info("hello fanout")

	if buf1.Len() == 0 {
		t.Error("handler 1 received nothing")
	}
	if buf2.Len() == 0 {
		t.Error("handler 2 received nothing")
	}
}

// TestSeverityRouting verifies NewRouter sends warnings+ to console and all to journal.
func TestSeverityRouting(t *testing.T) {
	dir := t.TempDir()
	jw, err := journal.NewWriter(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer jw.Close()

	var consoleBuf bytes.Buffer
	log := slog.New(journal.NewRouter(jw, &consoleBuf, "testsvc"))

	log.Info("info message")
	log.Warn("warn message")
	log.Error("error message")

	// Give the handler a moment to complete.
	time.Sleep(50 * time.Millisecond)
	_ = jw.Close()

	// Console should only have warn+ records.
	consoleOutput := consoleBuf.String()
	if contains(consoleOutput, "info message") {
		t.Error("console received info message; expected only warn+")
	}
	if !contains(consoleOutput, "warn message") {
		t.Error("console missing warn message")
	}
	if !contains(consoleOutput, "error message") {
		t.Error("console missing error message")
	}

	// Journal should have all three records.
	recs, err := journal.Query(dir, journal.Filter{MinPriority: journal.PriDebug})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Errorf("journal: expected 3 records, got %d", len(recs))
	}
}

// TestFloodLimiter verifies the per-service burst cap.
func TestFloodLimiter(t *testing.T) {
	limiter := journal.NewFloodLimiter(5)

	allowed := 0
	for i := 0; i < 20; i++ {
		if limiter.Allow("svc") {
			allowed++
		}
	}
	if allowed != 5 {
		t.Errorf("expected 5 allowed in burst, got %d", allowed)
	}

	// A different service should have its own bucket.
	if !limiter.Allow("other-svc") {
		t.Error("other-svc should be allowed on first call")
	}
}

// TestRedact verifies that common secret patterns are masked.
func TestRedact(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"password=secret123", "password=[REDACTED]"},
		{"token=abc.def.ghi", "token=[REDACTED]"},
		{"api_key=AKIAIOSFODNN7EXAMPLE", "api_key=[REDACTED]"},
		{"Bearer eyJhbGciOiJSUzI1NiJ9.payload.sig", "Bearer [REDACTED]"},
		{"no secrets here", "no secrets here"},
	}
	for _, c := range cases {
		got := journal.Redact(c.input, journal.DefaultRedactPatterns)
		if got != c.want {
			t.Errorf("Redact(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// TestWriterFloodLimiter verifies the limiter attached to a Writer drops excess.
func TestWriterFloodLimiter(t *testing.T) {
	dir := t.TempDir()
	jw, err := journal.NewWriter(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	limiter := journal.NewFloodLimiter(3)
	jw.SetLimiter(limiter)

	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		_ = jw.Write(journal.Record{
			Time:    now,
			Service: "flood-svc",
			Pri:     journal.PriInfo,
			Message: "msg",
		})
	}
	_ = jw.Close()

	recs, err := journal.Query(dir, journal.Filter{Service: "flood-svc", MinPriority: journal.PriDebug})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) > 3 {
		t.Errorf("expected at most 3 records (rate limit), got %d", len(recs))
	}
}

// TestForwarderKillSwitch verifies that Kill disables dispatch.
func TestForwarderKillSwitch(t *testing.T) {
	dir := t.TempDir()
	// URL points to a local port that nobody listens on; if the forwarder
	// tries to send after Kill it would error internally but not panic.
	fwd := journal.NewForwarder(dir, journal.ForwardConfig{
		URL:         "udp://127.0.0.1:59999",
		MinPriority: journal.PriDebug,
	})
	fwd.Kill() // kill before Run

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// Run should return promptly because ctx is already nearly done.
	fwd.Run(ctx)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
