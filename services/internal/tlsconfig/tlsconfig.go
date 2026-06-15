// Package tlsconfig provides centralized TLS configuration for NuraOS services.
//
// All outbound HTTPS connections must use NewTransport or NewClient to ensure:
//   - TLS verification is always enabled (no InsecureSkipVerify)
//   - Minimum protocol version is TLS 1.2
//   - The NuraOS CA bundle is used when the system pool is unavailable
//
// A failed CA bundle load is a hard error -- callers must not fall back to an
// unverified transport.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"
)

// BundlePath is the default CA certificate bundle path inside the initramfs.
const BundlePath = "/etc/ssl/certs/ca-certificates.crt"

// MinVersion is the minimum accepted TLS protocol version.
const MinVersion = tls.VersionTLS12

// NewTransport returns an *http.Transport with TLS verification enabled.
//
// caBundle may be:
//   - "" -- use the OS system certificate pool
//   - a file path -- load PEM certificates from that file
//
// TLS verification is always on. If the CA bundle cannot be loaded the
// function returns an error; callers must not proceed with an untrusted pool.
func NewTransport(caBundle string) (*http.Transport, error) {
	pool, err := loadPool(caBundle)
	if err != nil {
		return nil, fmt.Errorf("tlsconfig: CA pool: %w", err)
	}
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.TLSClientConfig = &tls.Config{
		MinVersion: MinVersion,
		RootCAs:    pool,
	}
	return base, nil
}

// NewClient returns an *http.Client using NewTransport with the given timeout.
func NewClient(caBundle string, timeout time.Duration) (*http.Client, error) {
	tr, err := NewTransport(caBundle)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: tr, Timeout: timeout}, nil
}

// CABundlePath returns the path to use for the CA bundle: the value of the
// NURA_CA_BUNDLE env var if set, otherwise BundlePath. Callers pass this to
// NewTransport/NewClient. If neither the env var path nor BundlePath exists
// on the current system, NewTransport falls back to the system pool.
func CABundlePath() string {
	if p := os.Getenv("NURA_CA_BUNDLE"); p != "" {
		return p
	}
	return BundlePath
}

func loadPool(path string) (*x509.CertPool, error) {
	if path == "" {
		pool, err := x509.SystemCertPool()
		if err != nil {
			// On some platforms (e.g. early-boot Linux without /etc/ssl), the
			// system pool is unavailable. Try the bundled path as a fallback.
			return loadPool(BundlePath)
		}
		return pool, nil
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		// File not present: fall back to system pool so offline-capable
		// development hosts (macOS, full Linux) work without the bundle.
		pool, serr := x509.SystemCertPool()
		if serr != nil {
			return nil, fmt.Errorf("bundle %s not found and system pool failed: %w; %w", path, err, serr)
		}
		return pool, nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("no valid PEM certificates in %s", path)
	}
	return pool, nil
}
