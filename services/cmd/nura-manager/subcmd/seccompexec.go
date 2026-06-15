// Package subcmd implements nura-manager sub-commands.
package subcmd

import (
	"fmt"
	"os"
	"syscall"

	"github.com/yasserrmd/nuraos/services/internal/seccomp"
)

// SeccompExec parses args of the form:
//
//	--profile <path> --mode <mode> -- <cmd> [args...]
//
// It loads the BPF allowlist profile, installs the seccomp filter, and then
// replaces the current process image with <cmd> via syscall.Exec. The filter
// is inherited by all subsequent exec calls in the privilege-drop chain.
func SeccompExec(args []string) {
	profilePath, mode, cmd, cmdArgs, err := parseSeccompExecArgs(args)
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

	if err := syscall.Exec(cmd, append([]string{cmd}, cmdArgs...), os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "nura-manager seccomp-exec: exec %q: %v\n", cmd, err)
		os.Exit(1)
	}
}

func parseSeccompExecArgs(args []string) (profile, mode, cmd string, cmdArgs []string, err error) {
	mode = "enforce"
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--profile":
			if i+1 >= len(args) {
				return "", "", "", nil, fmt.Errorf("--profile requires an argument")
			}
			profile = args[i+1]
			i += 2
		case "--mode":
			if i+1 >= len(args) {
				return "", "", "", nil, fmt.Errorf("--mode requires an argument")
			}
			mode = args[i+1]
			i += 2
		case "--":
			i++
			if i >= len(args) {
				return "", "", "", nil, fmt.Errorf("-- requires a command")
			}
			cmd = args[i]
			cmdArgs = args[i+1:]
			return
		default:
			return "", "", "", nil, fmt.Errorf("unknown flag %q", args[i])
		}
	}
	return "", "", "", nil, fmt.Errorf("missing -- <cmd>")
}
