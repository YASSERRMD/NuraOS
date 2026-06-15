package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimiterAllowsUnderBurst(t *testing.T) {
	rl := newRateLimiter(1.0, 5)
	for i := 0; i < 5; i++ {
		if !rl.allow("192.0.2.1") {
			t.Fatalf("request %d unexpectedly denied", i+1)
		}
	}
}

func TestRateLimiterBlocksOverBurst(t *testing.T) {
	rl := newRateLimiter(1.0, 3)
	for i := 0; i < 3; i++ {
		rl.allow("192.0.2.2")
	}
	if rl.allow("192.0.2.2") {
		t.Error("expected denial after burst exhausted")
	}
}

func TestRateLimiterSeparateIPsIndependent(t *testing.T) {
	rl := newRateLimiter(1.0, 1)
	rl.allow("192.0.2.10") // exhaust IP 10
	if !rl.allow("192.0.2.11") {
		t.Error("IP 11 should not be affected by IP 10's limit")
	}
}

func TestRateLimitMiddlewareHealthzExempt(t *testing.T) {
	rl := newRateLimiter(0, 0) // effectively blocks everything
	h := rateLimitMiddleware(http.HandlerFunc(okHandler), rl, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "192.0.2.1:9999"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("want 200 for /healthz, got %d", rr.Code)
	}
}

func TestRateLimitMiddlewareBlocks(t *testing.T) {
	rl := newRateLimiter(1.0, 1)
	rl.allow("192.0.2.3") // exhaust the single token
	h := rateLimitMiddleware(http.HandlerFunc(okHandler), rl, nil)
	req := httptest.NewRequest(http.MethodPost, "/chat", nil)
	req.RemoteAddr = "192.0.2.3:1234"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("want 429, got %d", rr.Code)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header missing on 429")
	}
}

func TestConcurrencyMiddlewareAllows(t *testing.T) {
	sem := make(chan struct{}, 2)
	h := concurrencyMiddleware(http.HandlerFunc(okHandler), sem, nil)
	req := httptest.NewRequest(http.MethodGet, "/tools", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rr.Code)
	}
}

func TestConcurrencyMiddlewareBlocks(t *testing.T) {
	sem := make(chan struct{}, 1)
	sem <- struct{}{} // fill the only slot
	h := concurrencyMiddleware(http.HandlerFunc(okHandler), sem, nil)
	req := httptest.NewRequest(http.MethodPost, "/chat", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("want 429 when at capacity, got %d", rr.Code)
	}
}

func TestConcurrencyMiddlewareHealthzExempt(t *testing.T) {
	sem := make(chan struct{}, 0) // cap 0: always "full"
	h := concurrencyMiddleware(http.HandlerFunc(okHandler), sem, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("want 200 for /healthz, got %d", rr.Code)
	}
}

func TestSecurityHeadersPresent(t *testing.T) {
	h := securityHeadersMiddleware(http.HandlerFunc(okHandler))
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	headers := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Content-Security-Policy": "default-src 'none'",
	}
	for key, want := range headers {
		if got := rr.Header().Get(key); got != want {
			t.Errorf("header %s: want %q, got %q", key, want, got)
		}
	}
}

func TestSecurityHeadersOnErrorResponse(t *testing.T) {
	// Verify security headers appear even on 401 responses.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	})
	h := securityHeadersMiddleware(inner)
	req := httptest.NewRequest(http.MethodPost, "/chat", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("X-Content-Type-Options missing on 401 response")
	}
}
