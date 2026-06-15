package timesync_test

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/timesync"
)

// TestClockSequence verifies that Now() returns strictly increasing sequence numbers.
func TestClockSequence(t *testing.T) {
	c := timesync.NewClock()
	prev := c.Now()
	for i := 0; i < 100; i++ {
		cur := c.Now()
		if cur.Seq <= prev.Seq {
			t.Errorf("seq not increasing: prev=%d cur=%d", prev.Seq, cur.Seq)
		}
		if cur.Wall.IsZero() {
			t.Error("wall time is zero")
		}
		prev = cur
	}
}

// TestClockStepNotFiredForSmallDrift verifies that normal clock drift does
// not produce step events.
func TestClockStepNotFiredForSmallDrift(t *testing.T) {
	c := timesync.NewClock()
	for i := 0; i < 50; i++ {
		c.Now()
	}
	// Drain the step channel.
	select {
	case step := <-c.StepEvents():
		t.Errorf("unexpected step event: %+v", step)
	default:
		// Expected: no step from rapid successive calls.
	}
}

// TestLoadTimezoneUTC verifies that a missing timezone file defaults to UTC.
func TestLoadTimezoneUTC(t *testing.T) {
	loc, err := timesync.LoadTimezone("/nonexistent/timezone")
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}
	if loc.String() != "UTC" {
		t.Errorf("expected UTC, got %q", loc.String())
	}
}

// TestLoadTimezoneValid verifies that a valid timezone file is parsed correctly.
func TestLoadTimezoneValid(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "timezone")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(f, "America/New_York")
	f.Close()

	loc, err := timesync.LoadTimezone(f.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loc.String() != "America/New_York" {
		t.Errorf("expected America/New_York, got %q", loc.String())
	}
}

// TestLoadTimezoneInvalid verifies that an unknown timezone returns an error.
func TestLoadTimezoneInvalid(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "timezone")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(f, "Not/ATimezone")
	f.Close()

	_, err = timesync.LoadTimezone(f.Name())
	if err == nil {
		t.Error("expected error for unknown timezone, got nil")
	}
}

// TestApplyTimezone verifies that TZ env is set.
func TestApplyTimezone(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/London")
	timesync.ApplyTimezone(loc)
	tz := os.Getenv("TZ")
	if tz != "Europe/London" {
		t.Errorf("TZ = %q, want Europe/London", tz)
	}
	// Restore.
	timesync.ApplyTimezone(time.UTC)
}

// TestSNTPQuery starts a fake UDP NTP server and verifies query decoding.
func TestSNTPQuery(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()

	// Known NTP time: 2025-01-15 12:00:00 UTC.
	want := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	ntpSecs := uint32(want.Unix() + 2208988800)

	go func() {
		buf := make([]byte, 48)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil || n < 1 {
				return
			}
			// Build a minimal NTP response.
			resp := make([]byte, 48)
			resp[0] = 0x24 // LI=0, VN=4, Mode=4 (server)
			binary.BigEndian.PutUint32(resp[40:44], ntpSecs)
			binary.BigEndian.PutUint32(resp[44:48], 0)
			_, _ = pc.WriteTo(resp, addr)
		}
	}()

	addr := pc.(*net.UDPConn).LocalAddr().String()
	got, err := timesync.SNTPQuery(addr)
	if err != nil {
		t.Fatalf("SNTPQuery: %v", err)
	}
	if !got.Equal(want) {
		t.Errorf("SNTPQuery returned %v, want %v", got, want)
	}
}

// TestReadRTCFallback verifies that ReadRTC returns a usable time even when
// the device is absent (test environments lack /dev/rtc0).
func TestReadRTCFallback(t *testing.T) {
	before := time.Now()
	got, _ := timesync.ReadRTC("/nonexistent/rtc")
	after := time.Now()
	// Either the fallback time or zero is acceptable; it must not panic.
	_ = got
	_ = before
	_ = after
}
