//go:build linux

package cap

import (
	"fmt"
	"syscall"
)

// prCapBsetDrop = PR_CAPBSET_DROP (24).
const prCapBsetDrop = 24

// dropBounding removes each capability in names from the bounding set.
func dropBounding(names []string) error {
	for _, name := range names {
		nr, ok := capabilityNumbers[name]
		if !ok {
			return fmt.Errorf("cap: unknown capability %q", name)
		}
		if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, prCapBsetDrop, nr, 0); errno != 0 {
			return fmt.Errorf("prctl(PR_CAPBSET_DROP, %s): %w", name, errno)
		}
	}
	return nil
}
