//go:build linux

package lifecycle

import (
	"github.com/yasserrmd/nuraos/services/internal/unit"
)

// seccompExecBin is the path to the seccomp-exec trampoline installed in the initramfs.
const seccompExecBin = "/sbin/nura-manager"

// wrapWithSeccomp prepends the seccomp-exec trampoline to args when the unit
// has a seccomp profile configured. The trampoline applies the BPF filter and
// then exec()s the remaining command; the filter is inherited by all subsequent
// exec calls in the privilege-drop chain (su -> sh -> service).
func wrapWithSeccomp(args []string, u *unit.Unit) []string {
	if u.Seccomp.Profile == "" {
		return args
	}
	mode := u.Seccomp.Mode
	if mode == "" {
		mode = "enforce"
	}
	return append(
		[]string{seccompExecBin, "seccomp-exec",
			"--profile", u.Seccomp.Profile,
			"--mode", mode,
			"--"},
		args...,
	)
}
