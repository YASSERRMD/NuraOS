package main

import (
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// tokenStore holds the current bearer token and allows hot-reload on SIGHUP.
// All methods are safe for concurrent use.
type tokenStore struct {
	mu    sync.RWMutex
	token string
	path  string
}

func newTokenStore(path string) *tokenStore {
	return &tokenStore{
		token: loadGatewayToken(path),
		path:  path,
	}
}

// get returns the current bearer token (empty string means auth disabled).
func (ts *tokenStore) get() string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.token
}

// reload reads the secrets file and updates the in-memory token.
// Called from the SIGHUP handler goroutine.
func (ts *tokenStore) reload() {
	tok := loadGatewayToken(ts.path)
	ts.mu.Lock()
	ts.token = tok
	ts.mu.Unlock()
	if tok != "" {
		slog.Info("gateway token reloaded from secrets file")
	} else {
		slog.Warn("gateway token cleared after reload (key absent or file removed)")
	}
}

// watchSIGHUP starts a goroutine that reloads the token on every SIGHUP.
// Call once at startup; the goroutine runs for the process lifetime.
func (ts *tokenStore) watchSIGHUP() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		for range ch {
			slog.Info("SIGHUP received: reloading secrets")
			ts.reload()
		}
	}()
}
