//go:build !linux

package identity

// SetHostname is a no-op on non-Linux platforms.
func SetHostname(hostname string) error { return nil }
