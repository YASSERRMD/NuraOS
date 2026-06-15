package discovery

import (
	"encoding/binary"
	"testing"
)

func TestEncodeName(t *testing.T) {
	tests := []struct {
		in   string
		want []byte
	}{
		{
			"_nura._tcp.local",
			[]byte{5, '_', 'n', 'u', 'r', 'a', 4, '_', 't', 'c', 'p', 5, 'l', 'o', 'c', 'a', 'l', 0},
		},
		{
			"a.b",
			[]byte{1, 'a', 1, 'b', 0},
		},
		{
			"x",
			[]byte{1, 'x', 0},
		},
	}
	for _, tc := range tests {
		got := encodeName(tc.in)
		if string(got) != string(tc.want) {
			t.Errorf("encodeName(%q) = %v; want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseName(t *testing.T) {
	name := "_nura._tcp.local"
	pkt := encodeName(name)
	got, off := parseName(pkt, 0)
	if got != name {
		t.Errorf("parseName = %q; want %q", got, name)
	}
	if off != len(pkt) {
		t.Errorf("off = %d; want %d", off, len(pkt))
	}
}

func TestIsQueryForService(t *testing.T) {
	r := &Responder{cfg: Config{Hostname: "nuraos", Port: 8080, InstanceName: "NuraOS"}}

	// Build a minimal DNS PTR query packet for _nura._tcp.local
	qname := encodeName("_nura._tcp.local")
	pkt := make([]byte, 12+len(qname)+4)
	// ID=0, flags=0x0000 (standard query), QDCOUNT=1
	binary.BigEndian.PutUint16(pkt[4:], 1)
	copy(pkt[12:], qname)
	off := 12 + len(qname)
	binary.BigEndian.PutUint16(pkt[off:], dnsTypePTR) // QTYPE
	binary.BigEndian.PutUint16(pkt[off+2:], dnsClassIN) // QCLASS

	if !r.isQueryForService(pkt) {
		t.Error("expected true for PTR query for _nura._tcp.local")
	}

	// A response packet (QR=1) must not match.
	pkt[2] = 0x80
	if r.isQueryForService(pkt) {
		t.Error("expected false for response packet (QR=1)")
	}
}

func TestBuildPacketHeader(t *testing.T) {
	r := &Responder{cfg: Config{Hostname: "nuraos", Port: 8080, InstanceName: "NuraOS"}}
	pkt := r.buildPacket()
	if len(pkt) < 12 {
		t.Fatalf("packet too short: %d bytes", len(pkt))
	}
	flags := binary.BigEndian.Uint16(pkt[2:4])
	if flags != 0x8400 {
		t.Errorf("flags = 0x%04X; want 0x8400 (QR+AA)", flags)
	}
	ancount := binary.BigEndian.Uint16(pkt[6:8])
	if ancount < 3 {
		t.Errorf("ANCOUNT = %d; want >= 3 (PTR+SRV+TXT)", ancount)
	}
}

func TestBuildTXT(t *testing.T) {
	entries := []string{"path=/healthz", "path=/v1/chat"}
	got := buildTXT(entries)
	if len(got) == 0 {
		t.Fatal("buildTXT returned empty slice")
	}
	// First byte is the length of the first entry.
	if int(got[0]) != len(entries[0]) {
		t.Errorf("first TXT entry length = %d; want %d", got[0], len(entries[0]))
	}
	// No internal socket paths or secrets present.
	s := string(got)
	for _, bad := range []string{"/run/", "sock", "key", "token", "secret"} {
		if containsInsensitive(s, bad) {
			t.Errorf("TXT RDATA contains sensitive string %q", bad)
		}
	}
}

func containsInsensitive(s, sub string) bool {
	return len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			match := true
			for j := 0; j < len(sub); j++ {
				c1, c2 := s[i+j], sub[j]
				if c1 >= 'A' && c1 <= 'Z' {
					c1 += 32
				}
				if c2 >= 'A' && c2 <= 'Z' {
					c2 += 32
				}
				if c1 != c2 {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
		return false
	}()
}
