//go:build !linux

package cap

import "fmt"

// dropBounding is a no-op on non-Linux platforms but still validates names.
func dropBounding(names []string) error {
	for _, name := range names {
		if _, ok := capabilityNumbers[name]; !ok {
			return fmt.Errorf("cap: unknown capability %q", name)
		}
	}
	return nil
}
