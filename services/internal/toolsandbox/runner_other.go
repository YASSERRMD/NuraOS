//go:build !linux

package toolsandbox

import "context"

// Run is not supported on non-Linux platforms.
func (r *Runner) Run(_ context.Context, _ Grant, _ string, _ ...string) (Result, error) {
	return Result{}, ErrNotSupported
}
