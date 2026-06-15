package tlsconfig_test

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/tlsconfig"
)

func TestNewTransportSystemPool(t *testing.T) {
	tr, err := tlsconfig.NewTransport("")
	if err != nil {
		t.Skipf("system cert pool not available: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil transport")
	}
	if tr.TLSClientConfig == nil {
		t.Fatal("expected TLSClientConfig to be set")
	}
	if tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d; want %d", tr.TLSClientConfig.MinVersion, tls.VersionTLS12)
	}
	if tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify must never be true")
	}
}

func TestNewTransportMissingBundle(t *testing.T) {
	// A non-existent bundle path should fall back to system pool (if available)
	// or return an error. It must never return a transport with InsecureSkipVerify.
	tr, err := tlsconfig.NewTransport("/nonexistent/ca-bundle.crt")
	if err != nil {
		// Acceptable: both bundle and system pool unavailable (e.g. container).
		t.Logf("expected error on missing bundle (no system pool): %v", err)
		return
	}
	if tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify must never be set as fallback")
	}
}

func TestNewClient(t *testing.T) {
	c, err := tlsconfig.NewClient("", 5*time.Second)
	if err != nil {
		t.Skipf("system cert pool not available: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected *http.Transport")
	}
	if tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d; want %d", tr.TLSClientConfig.MinVersion, tls.VersionTLS12)
	}
}

func TestCABundlePath(t *testing.T) {
	path := tlsconfig.CABundlePath()
	if path == "" {
		t.Error("CABundlePath must return a non-empty string")
	}
}
