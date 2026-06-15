package main

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

const defaultSecretsPath = "/data/etc/secrets.toml"

// loadGatewayToken reads gateway_token from a minimal TOML file.
// Returns "" if the file is absent or the key is not present.
func loadGatewayToken(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "gateway_token" {
			continue
		}
		val = strings.TrimSpace(val)
		if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
			val = val[1 : len(val)-1]
		}
		return val
	}
	return ""
}

// bearerAuthMiddleware requires a valid Bearer token on all endpoints except
// /healthz. When the store's token is empty the middleware is a no-op (auth
// disabled). The token is re-read from the store on every request so that a
// SIGHUP reload takes effect without restarting.
func bearerAuthMiddleware(next http.Handler, ts *tokenStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		tok := ts.get()
		if tok == "" {
			next.ServeHTTP(w, r)
			return
		}
		// Use constant-time comparison to avoid timing-based token enumeration.
		got := []byte(r.Header.Get("Authorization"))
		want := []byte("Bearer " + tok)
		if subtle.ConstantTimeCompare(got, want) != 1 {
			writeJSON(w, http.StatusUnauthorized,
				map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
