//go:build linux

package lifecycle

import (
	"github.com/yasserrmd/nuraos/services/internal/unit"
)

// seccompExecBin is the path to the seccomp-exec trampoline installed in the initramfs.
const seccompExecBin = "/sbin/nura-manager"

// wrapWithSeccomp prepends the seccomp-exec trampoline to args when the unit
// has a seccomp or landlock profile configured. The trampoline applies the BPF
// filter and/or Landlock ruleset, then exec()s the remaining command. Both are
// inherited by all subsequent exec calls in the privilege-drop chain
// (su -> sh -> service).
func wrapWithSeccomp(args []string, u *unit.Unit) []string {
	if u.Seccomp.Profile == "" && u.Landlock.Profile == "" {
		return args
	}
	mode := u.Seccomp.Mode
	if mode == "" {
		mode = "enforce"
	}
	prefix := []string{seccompExecBin, "seccomp-exec"}
	if u.Seccomp.Profile != "" {
		prefix = append(prefix, "--profile", u.Seccomp.Profile, "--mode", mode)
	}
	if u.Landlock.Profile != "" {
		prefix = append(prefix, "--landlock-profile", u.Landlock.Profile)
	}
	prefix = append(prefix, "--")
	return append(prefix, args...)
}
