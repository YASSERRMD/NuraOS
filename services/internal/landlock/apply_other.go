//go:build !linux

package landlock

// Apply is a no-op on non-Linux platforms.
func Apply(_ *Profile) error { return nil }
