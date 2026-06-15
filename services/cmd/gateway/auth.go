package main

import (
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
// /healthz. When token is empty the middleware is a no-op (auth disabled).
func bearerAuthMiddleware(next http.Handler, token string) http.Handler {
	if token == "" {
		return next
	}
	want := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Authorization") != want {
			writeJSON(w, http.StatusUnauthorized,
				map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
