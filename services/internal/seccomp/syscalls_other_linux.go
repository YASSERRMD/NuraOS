//go:build linux && !amd64

package seccomp

// syscallNumbers is empty on non-amd64 Linux; extend for other architectures.
var syscallNumbers = map[string]int{}
