package discovery

import (
	"context"
	"encoding/binary"
	"log/slog"
	"net"
	"strings"
	"time"
)

const (
	mdnsGroup        = "224.0.0.251"
	mdnsPort         = 5353
	announceInterval = 30 * time.Second

	dnsTypeA   = 1
	dnsTypePTR = 12
	dnsTypeTXT = 16
	dnsTypeSRV = 33
	dnsClassIN = 1
	dnsFlush   = 0x8000 // cache-flush bit for unique mDNS records
)

// Config controls what the mDNS responder advertises.
type Config struct {
	// Hostname is the local hostname without the .local suffix.
	Hostname string
	// Port is the TCP port the gateway listens on.
	Port uint16
	// InstanceName is the service instance label (default "NuraOS").
	InstanceName string
}

// Responder advertises the NuraOS gateway via mDNS (_nura._tcp.local).
// It sends gratuitous announcements on start and every 30 s, and responds to
// PTR queries for _nura._tcp.local. Auth is never bypassed; discovery only
// reveals the host:port of already-protected endpoints.
type Responder struct {
	cfg Config
	log *slog.Logger
}

// NewResponder returns a Responder. Call Start to begin advertising.
func NewResponder(cfg Config, log *slog.Logger) *Responder {
	if cfg.InstanceName == "" {
		cfg.InstanceName = "NuraOS"
	}
	return &Responder{cfg: cfg, log: log}
}

// Start runs the mDNS responder until ctx is cancelled.
// Bind failures are logged and silently ignored so boot continues.
func (r *Responder) Start(ctx context.Context) {
	maddr := &net.UDPAddr{IP: net.ParseIP(mdnsGroup), Port: mdnsPort}
	conn, err := net.ListenMulticastUDP("udp4", nil, maddr)
	if err != nil {
		r.log.Warn("mdns: cannot bind multicast; LAN discovery disabled", "err", err)
		return
	}

	// Close conn when ctx is cancelled to unblock the blocking Read below.
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	r.log.Info("mdns: advertising gateway",
		"service", "_nura._tcp.local",
		"instance", r.cfg.InstanceName,
		"port", r.cfg.Port)

	r.announce(conn)

	ticker := time.NewTicker(announceInterval)
	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.announce(conn)
			}
		}
	}()

	buf := make([]byte, 1500)
	for {
		n, _, readErr := conn.ReadFromUDP(buf)
		if readErr != nil {
			if ctx.Err() != nil {
				r.log.Info("mdns: discovery stopped")
			} else {
				r.log.Warn("mdns: read error", "err", readErr)
			}
			return
		}
		if r.isQueryForService(buf[:n]) {
			r.announce(conn)
		}
	}
}

func (r *Responder) announce(conn *net.UDPConn) {
	dest := &net.UDPAddr{IP: net.ParseIP(mdnsGroup), Port: mdnsPort}
	if _, err := conn.WriteTo(r.buildPacket(), dest); err != nil {
		r.log.Warn("mdns: send failed", "err", err)
	}
}

// isQueryForService returns true when pkt is a DNS PTR query for _nura._tcp.local.
func (r *Responder) isQueryForService(pkt []byte) bool {
	if len(pkt) < 12 {
		return false
	}
	flags := binary.BigEndian.Uint16(pkt[2:4])
	if flags&0x8000 != 0 { // QR bit 1 = response, not a query
		return false
	}
	if binary.BigEndian.Uint16(pkt[4:6]) == 0 { // QDCOUNT
		return false
	}
	name, next := parseName(pkt, 12)
	if next < 0 || next+2 > len(pkt) {
		return false
	}
	qtype := binary.BigEndian.Uint16(pkt[next : next+2])
	return qtype == dnsTypePTR && strings.EqualFold(name, "_nura._tcp.local")
}

// parseName decodes a DNS wire-format name from pkt starting at off.
// Returns the dotted label string and the offset after the name (past root or pointer).
func parseName(pkt []byte, off int) (string, int) {
	var labels []string
	for {
		if off >= len(pkt) {
			return "", -1
		}
		llen := int(pkt[off])
		if llen == 0 {
			return strings.Join(labels, "."), off + 1
		}
		if llen&0xC0 == 0xC0 { // compression pointer
			if off+1 >= len(pkt) {
				return "", -1
			}
			ptr := int(binary.BigEndian.Uint16(pkt[off:off+2]) & 0x3FFF)
			if ptr >= off { // only back-pointers are valid in standard DNS
				return "", -1
			}
			rest, ok := parseName(pkt, ptr)
			if ok < 0 {
				return "", -1
			}
			if rest != "" {
				labels = append(labels, strings.Split(rest, ".")...)
			}
			return strings.Join(labels, "."), off + 2
		}
		off++
		if off+llen > len(pkt) {
			return "", -1
		}
		labels = append(labels, string(pkt[off:off+llen]))
		off += llen
	}
}

// buildPacket returns an mDNS response advertising the gateway.
// Only /healthz and /v1/chat are included in TXT records; no secrets
// or internal socket paths are advertised.
func (r *Responder) buildPacket() []byte {
	instance := r.cfg.InstanceName + "._nura._tcp.local"
	host := r.cfg.Hostname + ".local"
	localIP := firstIPv4()

	ans := &msgBuf{}
	count := 0

	// PTR: _nura._tcp.local -> instance (shared record, no flush bit)
	ans.name("_nura._tcp.local")
	ans.u16(dnsTypePTR)
	ans.u16(dnsClassIN)
	ans.u32(120)
	ptr := encodeName(instance)
	ans.u16(uint16(len(ptr)))
	ans.raw(ptr)
	count++

	// SRV: instance -> host:port (unique record)
	ans.name(instance)
	ans.u16(dnsTypeSRV)
	ans.u16(dnsClassIN | dnsFlush)
	ans.u32(120)
	target := encodeName(host)
	srv := make([]byte, 6, 6+len(target))
	binary.BigEndian.PutUint16(srv[0:], 0) // priority
	binary.BigEndian.PutUint16(srv[2:], 0) // weight
	binary.BigEndian.PutUint16(srv[4:], r.cfg.Port)
	srv = append(srv, target...)
	ans.u16(uint16(len(srv)))
	ans.raw(srv)
	count++

	// TXT: instance -> advertised endpoints only
	ans.name(instance)
	ans.u16(dnsTypeTXT)
	ans.u16(dnsClassIN | dnsFlush)
	ans.u32(4500)
	txt := buildTXT([]string{"path=/healthz", "path=/v1/chat"})
	ans.u16(uint16(len(txt)))
	ans.raw(txt)
	count++

	// A: host -> local IPv4 (unique, omitted when no routable address found)
	if localIP != nil {
		ans.name(host)
		ans.u16(dnsTypeA)
		ans.u16(dnsClassIN | dnsFlush)
		ans.u32(120)
		ans.u16(4)
		ans.raw(localIP.To4())
		count++
	}

	hdr := &msgBuf{}
	hdr.u16(0)            // ID: 0 for mDNS
	hdr.u16(0x8400)       // QR=1 (response), AA=1 (authoritative)
	hdr.u16(0)            // QDCOUNT
	hdr.u16(uint16(count))
	hdr.u16(0) // NSCOUNT
	hdr.u16(0) // ARCOUNT

	return append(hdr.out, ans.out...)
}

// encodeName encodes a dotted DNS name as length-prefixed labels.
func encodeName(name string) []byte {
	name = strings.TrimSuffix(name, ".")
	var b []byte
	for _, label := range strings.Split(name, ".") {
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	return append(b, 0) // root label
}

// buildTXT returns DNS TXT RDATA from a list of strings.
func buildTXT(entries []string) []byte {
	var b []byte
	for _, e := range entries {
		b = append(b, byte(len(e)))
		b = append(b, e...)
	}
	return b
}

// firstIPv4 returns the first non-loopback IPv4 address on any interface.
func firstIPv4() net.IP {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if v4 := ipnet.IP.To4(); v4 != nil {
			return v4
		}
	}
	return nil
}

// msgBuf is a minimal byte builder for constructing DNS wire-format messages.
type msgBuf struct{ out []byte }

func (b *msgBuf) u16(v uint16) { b.out = append(b.out, byte(v>>8), byte(v)) }
func (b *msgBuf) u32(v uint32) {
	b.out = append(b.out, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
func (b *msgBuf) raw(p []byte) { b.out = append(b.out, p...) }
func (b *msgBuf) name(s string) { b.raw(encodeName(s)) }
