//go:build linux

package lifecycle

import (
	"syscall"

	"github.com/yasserrmd/nuraos/services/internal/unit"
)

// applyNamespaces sets Cloneflags on attr from the unit's namespace config.
// Each flag is additive; Setpgid must already be set by the caller.
func applyNamespaces(attr *syscall.SysProcAttr, ns unit.Namespaces) {
	if ns.IPC {
		attr.Cloneflags |= syscall.CLONE_NEWIPC
	}
	if ns.UTS {
		attr.Cloneflags |= syscall.CLONE_NEWUTS
	}
	if ns.PID {
		attr.Cloneflags |= syscall.CLONE_NEWPID
	}
	if ns.Network {
		attr.Cloneflags |= syscall.CLONE_NEWNET
	}
	if ns.Mount {
		attr.Cloneflags |= syscall.CLONE_NEWNS
	}
}
