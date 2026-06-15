//go:build linux

package identity

import "syscall"

// SetHostname applies hostname to the running kernel via sethostname(2).
// Requires CAP_SYS_ADMIN; errors are non-fatal (caller logs and continues).
func SetHostname(hostname string) error {
	return syscall.Sethostname([]byte(hostname))
}
