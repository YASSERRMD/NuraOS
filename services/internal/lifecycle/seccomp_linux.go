//go:build linux

package lifecycle

import (
	"strings"

	"github.com/yasserrmd/nuraos/services/internal/unit"
)

// seccompExecBin is the path to the seccomp-exec trampoline installed in the initramfs.
const seccompExecBin = "/sbin/nura-manager"

// wrapWithSeccomp prepends the seccomp-exec trampoline to args when the unit
// has a seccomp profile, landlock profile, or capability bounding-set drops
// configured. The trampoline applies each restriction in order (cap bounding
// drop, seccomp BPF filter, Landlock ruleset), then exec()s the remaining
// command. All restrictions are inherited by the su/sh/service exec chain.
func wrapWithSeccomp(args []string, u *unit.Unit) []string {
	hasSeccomp := u.Seccomp.Profile != ""
	hasLandlock := u.Landlock.Profile != ""
	hasCaps := len(u.Capabilities.BoundingDrop) > 0
	if !hasSeccomp && !hasLandlock && !hasCaps {
		return args
	}
	mode := u.Seccomp.Mode
	if mode == "" {
		mode = "enforce"
	}
	prefix := []string{seccompExecBin, "seccomp-exec"}
	if hasSeccomp {
		prefix = append(prefix, "--profile", u.Seccomp.Profile, "--mode", mode)
	}
	if hasLandlock {
		prefix = append(prefix, "--landlock-profile", u.Landlock.Profile)
	}
	if hasCaps {
		prefix = append(prefix, "--cap-drop", strings.Join(u.Capabilities.BoundingDrop, ","))
	}
	prefix = append(prefix, "--")
	return append(prefix, args...)
}
