//go:build !race

package seccomp_test

func raceEnabled() bool { return false }
