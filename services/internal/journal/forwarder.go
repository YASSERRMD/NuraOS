package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

// ForwardConfig holds the configuration for optional log forwarding.
// Forwarding is disabled when URL is empty.
type ForwardConfig struct {
	// URL is the remote endpoint. Supported schemes:
	//   udp://host:port        RFC 5424 syslog over UDP
	//   http://host/path       HTTP JSON POST (one record per request)
	//   https://host/path      HTTPS JSON POST
	// Empty or unset disables forwarding.
	URL string

	// MinPriority is the minimum severity forwarded. Defaults to PriWarning.
	// Records with higher numeric value (lower severity) are dropped.
	MinPriority Priority

	// RedactPatterns replaces DefaultRedactPatterns when non-nil.
	RedactPatterns []*regexp.Regexp

	// Service restricts forwarding to a single service; empty means all.
	Service string
}

// Forwarder tails the journal and forwards matching records to a remote
// endpoint with optional secret redaction.
//
// It respects two kill switches:
//   - F.Kill() called programmatically (sets an atomic flag)
//   - The file <journal-dir>/no-forward existing on disk
//
// Either kill switch permanently disables forwarding for this Forwarder.
type Forwarder struct {
	dir     string
	cfg     ForwardConfig
	stopped int32 // atomic; 1 = disabled
}

// NewForwarder creates a Forwarder. Forwarding only begins when Run is called.
// cfg.URL == "" is a no-op Run.
func NewForwarder(dir string, cfg ForwardConfig) *Forwarder {
	if cfg.MinPriority == 0 {
		cfg.MinPriority = PriWarning
	}
	return &Forwarder{dir: dir, cfg: cfg}
}

// Kill permanently disables forwarding for this Forwarder instance.
// It is safe to call concurrently with Run.
func (f *Forwarder) Kill() { atomic.StoreInt32(&f.stopped, 1) }

// Run follows the journal and forwards records until ctx is cancelled.
// It returns as soon as ctx.Done() fires or either kill switch is activated.
func (f *Forwarder) Run(ctx context.Context) {
	if f.cfg.URL == "" {
		return
	}
	patterns := f.cfg.RedactPatterns
	if patterns == nil {
		patterns = DefaultRedactPatterns
	}
	stopCh := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(stopCh)
	}()

	filter := Filter{
		Service:     f.cfg.Service,
		MinPriority: f.cfg.MinPriority,
	}

	Follow(f.dir, filter, stopCh, func(r Record) {
		if atomic.LoadInt32(&f.stopped) == 1 {
			return
		}
		if f.killFilePresent() {
			atomic.StoreInt32(&f.stopped, 1)
			return
		}
		r.Message = Redact(r.Message, patterns)
		f.dispatch(r)
	})
}

func (f *Forwarder) killFilePresent() bool {
	_, err := os.Stat(filepath.Join(f.dir, "no-forward"))
	return err == nil
}

func (f *Forwarder) dispatch(r Record) {
	u := f.cfg.URL
	if strings.HasPrefix(u, "udp://") {
		f.sendSyslog(r, u[6:])
	} else {
		f.sendHTTP(r, u)
	}
}

// sendSyslog sends one RFC 5424 syslog message over UDP.
func (f *Forwarder) sendSyslog(r Record, addr string) {
	conn, err := net.DialTimeout("udp", addr, 2*time.Second)
	if err != nil {
		return
	}
	defer conn.Close()
	// PRI = facility * 8 + severity; use LOG_USER (facility 1).
	pri := 1*8 + int(r.Pri)
	msg := fmt.Sprintf("<%d>1 %s - %s %d - - %s\n",
		pri, r.Time.UTC().Format(time.RFC3339), r.Service, r.PID, r.Message)
	_, _ = conn.Write([]byte(msg))
}

// sendHTTP posts one record as JSON to the configured HTTP endpoint.
func (f *Forwarder) sendHTTP(r Record, url string) {
	body, err := json.Marshal(r)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return
	}
	resp.Body.Close()
}
