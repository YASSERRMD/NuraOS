// Package subcmd implements nura-manager sub-commands.
package subcmd

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/yasserrmd/nuraos/services/internal/cap"
	"github.com/yasserrmd/nuraos/services/internal/landlock"
	"github.com/yasserrmd/nuraos/services/internal/seccomp"
)

// SeccompExec parses args of the form:
//
//	--profile <path> --mode <mode> --landlock-profile <path> --cap-drop <caps> -- <cmd> [args...]
//
// Order of operations (must match this sequence for correctness):
//  1. Drop capabilities from the bounding set (--cap-drop). Must happen first
//     because dropping bounding caps requires CAP_SETPCAP, which is lost after
//     the seccomp filter may restrict prctl.
//  2. Apply the seccomp BPF filter (--profile). Restricts available syscalls.
//  3. Apply the Landlock filesystem ruleset (--landlock-profile). Restricts
//     path access; non-fatal on kernels older than 5.13.
//  4. exec(2) into the target command. All restrictions are inherited.
func SeccompExec(args []string) {
	opts, err := parseSeccompExecArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nura-manager seccomp-exec: %v\n", err)
		os.Exit(1)
	}

	// Step 1: capability bounding set trimming.
	if len(opts.capDrop) > 0 {
		if err := cap.Drop(opts.capDrop); err != nil {
			fmt.Fprintf(os.Stderr, "nura-manager seccomp-exec: cap drop: %v\n", err)
			os.Exit(1)
		}
	}

	// Step 2: seccomp BPF filter.
	var profile *seccomp.Profile
	if opts.profilePath != "" {
		profile, err = seccomp.Load(opts.profilePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "nura-manager seccomp-exec: %v\n", err)
			os.Exit(1)
		}
	}
	if err := seccomp.Apply(profile, seccomp.Mode(opts.mode)); err != nil {
		fmt.Fprintf(os.Stderr, "nura-manager seccomp-exec: apply seccomp: %v\n", err)
		os.Exit(1)
	}

	// Step 3: Landlock filesystem confinement (non-fatal on older kernels).
	if opts.landlockProfile != "" {
		ll, err := landlock.Load(opts.landlockProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "nura-manager seccomp-exec: landlock load: %v\n", err)
			os.Exit(1)
		}
		if err := landlock.Apply(ll); err != nil {
			fmt.Fprintf(os.Stderr, "nura-manager seccomp-exec: landlock apply: %v\n", err)
		}
	}

	// Step 4: replace this process image with the target command.
	if err := syscall.Exec(opts.cmd, append([]string{opts.cmd}, opts.cmdArgs...), os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "nura-manager seccomp-exec: exec %q: %v\n", opts.cmd, err)
		os.Exit(1)
	}
}

type seccompExecOpts struct {
	profilePath     string
	mode            string
	landlockProfile string
	capDrop         []string
	cmd             string
	cmdArgs         []string
}

func parseSeccompExecArgs(args []string) (seccompExecOpts, error) {
	opts := seccompExecOpts{mode: "enforce"}
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--profile":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--profile requires an argument")
			}
			opts.profilePath = args[i+1]
			i += 2
		case "--mode":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--mode requires an argument")
			}
			opts.mode = args[i+1]
			i += 2
		case "--landlock-profile":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--landlock-profile requires an argument")
			}
			opts.landlockProfile = args[i+1]
			i += 2
		case "--cap-drop":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--cap-drop requires an argument")
			}
			opts.capDrop = strings.Split(args[i+1], ",")
			i += 2
		case "--":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("-- requires a command")
			}
			opts.cmd = args[i]
			opts.cmdArgs = args[i+1:]
			return opts, nil
		default:
			return opts, fmt.Errorf("unknown flag %q", args[i])
		}
	}
	return opts, fmt.Errorf("missing -- <cmd>")
}
