package timesync

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// ntpEpochOffset is the number of seconds between the NTP epoch (1 Jan 1900)
// and the Unix epoch (1 Jan 1970).
const ntpEpochOffset = 2208988800

// SNTPQuery sends a single SNTP request to server and returns the server's
// current UTC time. server may be a hostname, host:port, or IP:port.
// Port 123 is assumed when absent.
//
// This is a simplified one-shot client per RFC 4330. It is opt-in and is
// never required for offline boot.
func SNTPQuery(server string) (time.Time, error) {
	addr := server
	if _, _, err := net.SplitHostPort(server); err != nil {
		addr = net.JoinHostPort(server, "123")
	}

	conn, err := net.DialTimeout("udp", addr, 5*time.Second)
	if err != nil {
		return time.Time{}, fmt.Errorf("sntp dial %s: %w", addr, err)
	}
	defer conn.Close()

	// NTP/SNTP request: LI=0, VN=3, Mode=3 (client).
	req := make([]byte, 48)
	req[0] = 0x1b

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(req); err != nil {
		return time.Time{}, fmt.Errorf("sntp write: %w", err)
	}

	resp := make([]byte, 48)
	if _, err := conn.Read(resp); err != nil {
		return time.Time{}, fmt.Errorf("sntp read: %w", err)
	}

	// Transmit timestamp: bytes 40-47 (NTP seconds | fraction, big-endian).
	secs := binary.BigEndian.Uint32(resp[40:44])
	frac := binary.BigEndian.Uint32(resp[44:48])
	ns := (int64(frac) * 1e9) >> 32
	return time.Unix(int64(secs)-ntpEpochOffset, ns).UTC(), nil
}
