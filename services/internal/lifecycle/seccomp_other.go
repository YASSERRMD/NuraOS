//go:build !linux

package lifecycle

import (
	"github.com/yasserrmd/nuraos/services/internal/unit"
)

// wrapWithSeccomp is a no-op on non-Linux platforms.
func wrapWithSeccomp(args []string, _ *unit.Unit) []string {
	return args
}
