package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSecretsFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.toml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestLoadGatewayTokenMissing(t *testing.T) {
	tok := loadGatewayToken("/nonexistent-p30-secrets.toml")
	if tok != "" {
		t.Errorf("want empty, got %q", tok)
	}
}

func TestLoadGatewayTokenPresent(t *testing.T) {
	path := writeSecretsFile(t, `# comment
gateway_token = "mysecret"
anthropic_api_key = "sk-test"
`)
	tok := loadGatewayToken(path)
	if tok != "mysecret" {
		t.Errorf("want 'mysecret', got %q", tok)
	}
}

func TestLoadGatewayTokenUnquoted(t *testing.T) {
	path := writeSecretsFile(t, "gateway_token = hello\n")
	tok := loadGatewayToken(path)
	if tok != "hello" {
		t.Errorf("want 'hello', got %q", tok)
	}
}

func TestLoadGatewayTokenNotSet(t *testing.T) {
	path := writeSecretsFile(t, "anthropic_api_key = \"sk-abc\"\n")
	tok := loadGatewayToken(path)
	if tok != "" {
		t.Errorf("want empty, got %q", tok)
	}
}

func okHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func TestAuthMiddlewareDisabled(t *testing.T) {
	h := bearerAuthMiddleware(http.HandlerFunc(okHandler), "")
	req := httptest.NewRequest(http.MethodGet, "/chat", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("want 200 when auth disabled, got %d", rr.Code)
	}
}

func TestAuthMiddlewareRejectsUnauthorized(t *testing.T) {
	h := bearerAuthMiddleware(http.HandlerFunc(okHandler), "secret123")
	req := httptest.NewRequest(http.MethodPost, "/chat", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

func TestAuthMiddlewareRejectsWrongToken(t *testing.T) {
	h := bearerAuthMiddleware(http.HandlerFunc(okHandler), "correct")
	req := httptest.NewRequest(http.MethodPost, "/chat", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

func TestAuthMiddlewareAcceptsValidToken(t *testing.T) {
	h := bearerAuthMiddleware(http.HandlerFunc(okHandler), "secret123")
	req := httptest.NewRequest(http.MethodPost, "/chat", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rr.Code)
	}
}

func TestAuthMiddlewareHealthzExempt(t *testing.T) {
	h := bearerAuthMiddleware(http.HandlerFunc(okHandler), "secret123")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	// No Authorization header.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("want 200 for /healthz without auth, got %d", rr.Code)
	}
}

func TestAuthMiddlewareSetsUnauthorizedBody(t *testing.T) {
	h := bearerAuthMiddleware(http.HandlerFunc(okHandler), "tok")
	req := httptest.NewRequest(http.MethodGet, "/tools", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if !strings.Contains(rr.Body.String(), "unauthorized") {
		t.Errorf("body %q: missing 'unauthorized'", rr.Body.String())
	}
}
