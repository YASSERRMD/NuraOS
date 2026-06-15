// Package subcmd implements nura-manager sub-commands.
package subcmd

import (
	"fmt"
	"os"
	"syscall"

	"github.com/yasserrmd/nuraos/services/internal/landlock"
	"github.com/yasserrmd/nuraos/services/internal/seccomp"
)

// SeccompExec parses args of the form:
//
//	--profile <path> --mode <mode> --landlock-profile <path> -- <cmd> [args...]
//
// It loads the BPF allowlist profile (if any), installs the seccomp filter,
// loads the Landlock profile (if any), installs the filesystem confinement
// ruleset, and then replaces the current process image with <cmd> via
// syscall.Exec. Both filters are inherited by all subsequent exec calls in the
// privilege-drop chain.
func SeccompExec(args []string) {
	profilePath, mode, landlockProfile, cmd, cmdArgs, err := parseSeccompExecArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nura-manager seccomp-exec: %v\n", err)
		os.Exit(1)
	}

	var profile *seccomp.Profile
	if profilePath != "" {
		profile, err = seccomp.Load(profilePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "nura-manager seccomp-exec: %v\n", err)
			os.Exit(1)
		}
	}

	if err := seccomp.Apply(profile, seccomp.Mode(mode)); err != nil {
		fmt.Fprintf(os.Stderr, "nura-manager seccomp-exec: apply filter: %v\n", err)
		os.Exit(1)
	}

	if landlockProfile != "" {
		ll, err := landlock.Load(landlockProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "nura-manager seccomp-exec: landlock load: %v\n", err)
			os.Exit(1)
		}
		// Non-fatal: older kernels (< 5.13) do not support Landlock.
		if err := landlock.Apply(ll); err != nil {
			fmt.Fprintf(os.Stderr, "nura-manager seccomp-exec: landlock apply: %v\n", err)
		}
	}

	if err := syscall.Exec(cmd, append([]string{cmd}, cmdArgs...), os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "nura-manager seccomp-exec: exec %q: %v\n", cmd, err)
		os.Exit(1)
	}
}

func parseSeccompExecArgs(args []string) (profile, mode, landlockProfile, cmd string, cmdArgs []string, err error) {
	mode = "enforce"
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--profile":
			if i+1 >= len(args) {
				return "", "", "", "", nil, fmt.Errorf("--profile requires an argument")
			}
			profile = args[i+1]
			i += 2
		case "--mode":
			if i+1 >= len(args) {
				return "", "", "", "", nil, fmt.Errorf("--mode requires an argument")
			}
			mode = args[i+1]
			i += 2
		case "--landlock-profile":
			if i+1 >= len(args) {
				return "", "", "", "", nil, fmt.Errorf("--landlock-profile requires an argument")
			}
			landlockProfile = args[i+1]
			i += 2
		case "--":
			i++
			if i >= len(args) {
				return "", "", "", "", nil, fmt.Errorf("-- requires a command")
			}
			cmd = args[i]
			cmdArgs = args[i+1:]
			return
		default:
			return "", "", "", "", nil, fmt.Errorf("unknown flag %q", args[i])
		}
	}
	return "", "", "", "", nil, fmt.Errorf("missing -- <cmd>")
}
