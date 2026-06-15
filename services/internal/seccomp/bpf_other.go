//go:build !linux

package seccomp

// Apply is a no-op on non-Linux platforms.
func Apply(_ *Profile, _ Mode) error { return nil }

func resolveNames(names []string) (nrs []uint32, unknown []string) {
	return nil, nil
}
