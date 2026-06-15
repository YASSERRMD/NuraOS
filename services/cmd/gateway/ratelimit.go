package main

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultRPS      = 1.0            // allowed requests per second per client IP
	defaultBurst    = float64(10)    // token-bucket burst capacity
	maxConcurrent   = 4              // simultaneous non-health requests
	cleanupInterval = 5 * time.Minute
	staleDuration   = 5 * time.Minute
)

// ipBucket is a per-client token-bucket entry.
type ipBucket struct {
	tokens   float64
	lastSeen time.Time
}

// rateLimiter implements per-IP token-bucket rate limiting.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*ipBucket
	rps     float64
	burst   float64
}

func newRateLimiter(rps, burst float64) *rateLimiter {
	rl := &rateLimiter{
		buckets: make(map[string]*ipBucket),
		rps:     rps,
		burst:   burst,
	}
	go rl.periodicCleanup()
	return rl
}

// allow returns true and consumes one token if the IP is within its rate limit.
func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	b, ok := rl.buckets[ip]
	if !ok {
		rl.buckets[ip] = &ipBucket{tokens: rl.burst - 1, lastSeen: now}
		return true
	}
	b.tokens += now.Sub(b.lastSeen).Seconds() * rl.rps
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.lastSeen = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

func (rl *rateLimiter) periodicCleanup() {
	t := time.NewTicker(cleanupInterval)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-staleDuration)
		rl.mu.Lock()
		for ip, b := range rl.buckets {
			if b.lastSeen.Before(cutoff) {
				delete(rl.buckets, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// clientIP returns the originating IP. Respects X-Real-IP set by a proxy.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i > 0 {
		return addr[:i]
	}
	return addr
}

// rateLimitMiddleware enforces per-IP request rate. /healthz is exempt.
// m may be nil; counter increments are no-ops on a nil MetricsStore.
func rateLimitMiddleware(next http.Handler, rl *rateLimiter, m *MetricsStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if !rl.allow(clientIP(r)) {
			m.incRateLimited()
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusTooManyRequests,
				map[string]string{"error": "rate limit exceeded"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// concurrencyMiddleware caps concurrent non-health requests using a semaphore.
// Requests that cannot acquire a slot immediately receive 429.
// m may be nil; counter increments are no-ops on a nil MetricsStore.
func concurrencyMiddleware(next http.Handler, sem chan struct{}, m *MetricsStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		default:
			m.incConcurrencyBusy()
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusTooManyRequests,
				map[string]string{"error": "server busy"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// securityHeadersMiddleware adds defensive HTTP response headers to all responses.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}
