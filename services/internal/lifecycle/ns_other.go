//go:build !linux

package lifecycle

import (
	"syscall"

	"github.com/yasserrmd/nuraos/services/internal/unit"
)

// applyNamespaces is a no-op on non-Linux platforms.
func applyNamespaces(_ *syscall.SysProcAttr, _ unit.Namespaces) {}
