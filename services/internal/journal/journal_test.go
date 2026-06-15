package journal_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/journal"
)

func TestWriteAndQuery(t *testing.T) {
	dir := t.TempDir()
	w, err := journal.NewWriter(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	now := time.Now().UTC()
	records := []journal.Record{
		{Time: now.Add(-2 * time.Minute), Service: "gateway", PID: 100, Pri: journal.PriInfo, Message: "started"},
		{Time: now.Add(-1 * time.Minute), Service: "gateway", PID: 100, Pri: journal.PriError, Message: "error occurred"},
		{Time: now, Service: "nura-agent", PID: 200, Pri: journal.PriInfo, Message: "connected"},
	}
	for _, r := range records {
		if err := w.Write(r); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	_ = w.Close()

	// Query all.
	all, err := journal.Query(dir, journal.Filter{MinPriority: journal.PriDebug})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 records, got %d", len(all))
	}

	// Filter by service.
	gw, err := journal.Query(dir, journal.Filter{Service: "gateway", MinPriority: journal.PriDebug})
	if err != nil {
		t.Fatal(err)
	}
	if len(gw) != 2 {
		t.Errorf("expected 2 gateway records, got %d", len(gw))
	}

	// Filter by priority (errors only).
	errs, err := journal.Query(dir, journal.Filter{MinPriority: journal.PriError})
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 1 {
		t.Errorf("expected 1 error record, got %d", len(errs))
	}
}

func TestTail(t *testing.T) {
	dir := t.TempDir()
	w, err := journal.NewWriter(dir, 0)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		_ = w.Write(journal.Record{
			Time:    now.Add(time.Duration(i) * time.Second),
			Service: "svc",
			Pri:     journal.PriInfo,
			Message: fmt.Sprintf("msg %d", i),
		})
	}
	_ = w.Close()

	tail, err := journal.Tail(dir, 3, journal.Filter{MinPriority: journal.PriDebug})
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 3 {
		t.Errorf("expected 3 tail records, got %d", len(tail))
	}
	if tail[2].Message != "msg 9" {
		t.Errorf("last tail record = %q, want 'msg 9'", tail[2].Message)
	}
}

func TestRotation(t *testing.T) {
	dir := t.TempDir()
	// Very small cap: 256 bytes.
	w, err := journal.NewWriter(dir, 256)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	for i := 0; i < 20; i++ {
		_ = w.Write(journal.Record{
			Time:    now.Add(time.Duration(i) * time.Hour * 24),
			Service: "svc",
			Pri:     journal.PriInfo,
			Message: fmt.Sprintf("line %d with some padding text to make it larger", i),
		})
	}
	_ = w.Close()

	// Check that at least some files were written.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Error("expected journal files, got none")
	}
}

func TestPriorityString(t *testing.T) {
	cases := []struct{ p journal.Priority; s string }{
		{journal.PriEmergency, "emergency"},
		{journal.PriInfo, "info"},
		{journal.PriDebug, "debug"},
	}
	for _, c := range cases {
		if got := c.p.String(); got != c.s {
			t.Errorf("Priority(%d).String() = %q, want %q", c.p, got, c.s)
		}
	}
}

func TestCollect(t *testing.T) {
	dir := t.TempDir()
	w, err := journal.NewWriter(dir, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Use a pipe as a mock service output stream.
	r, pw, _ := os.Pipe()
	done := make(chan struct{})
	go func() {
		journal.Collect(r, w, "testsvc", 9999, journal.PriInfo)
		close(done)
	}()

	fmt.Fprintln(pw, "line one")
	fmt.Fprintln(pw, "line two")
	pw.Close()
	<-done
	_ = w.Close()

	recs, err := journal.Query(dir, journal.Filter{Service: "testsvc", MinPriority: journal.PriDebug})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Errorf("expected 2 collected records, got %d", len(recs))
	}
}

func TestServiceNames(t *testing.T) {
	dir := t.TempDir()
	w, _ := journal.NewWriter(dir, 0)
	now := time.Now().UTC()
	_ = w.Write(journal.Record{Time: now, Service: "alpha", Pri: journal.PriInfo, Message: "a"})
	_ = w.Write(journal.Record{Time: now, Service: "beta", Pri: journal.PriInfo, Message: "b"})
	_ = w.Write(journal.Record{Time: now, Service: "alpha", Pri: journal.PriInfo, Message: "c"})
	_ = w.Close()

	names, err := journal.ServiceNames(dir)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, n := range names {
		seen[n] = true
	}
	if !seen["alpha"] || !seen["beta"] {
		t.Errorf("expected alpha and beta in %v", names)
	}
}
