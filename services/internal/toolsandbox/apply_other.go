//go:build !linux

package toolsandbox

// MaybeApplyAndExec is a no-op on non-Linux platforms.
// It always returns false and the caller continues normally.
func MaybeApplyAndExec() bool { return false }
