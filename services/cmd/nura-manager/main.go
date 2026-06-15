// Command nura-manager is the NuraOS service manager.
//
// It reads declarative unit files from /etc/nura/services/*.toml, resolves
// dependency order, and starts services in the correct sequence with readiness
// gating between dependent stages.
//
// Usage:
//
//	nura-manager run          # start all enabled units (normal mode)
//	nura-manager dry-run      # print the computed start plan and exit
//	nura-manager check        # validate all unit files and exit
package main

import (
	"fmt"
	"os"

	"github.com/yasserrmd/nuraos/services/cmd/nura-manager/subcmd"
)

const defaultUnitDir = "/etc/nura/services"

func main() {
	unitDir := os.Getenv("NURA_UNIT_DIR")
	if unitDir == "" {
		unitDir = defaultUnitDir
	}

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		if err := subcmd.Run(unitDir); err != nil {
			fmt.Fprintf(os.Stderr, "nura-manager: %v\n", err)
			os.Exit(1)
		}
	case "dry-run":
		if err := subcmd.DryRun(unitDir, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "nura-manager: %v\n", err)
			os.Exit(1)
		}
	case "check":
		if err := subcmd.Check(unitDir, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "nura-manager: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "nura-manager: unknown command %q\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: nura-manager <command>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  run      Start all enabled units in dependency order")
	fmt.Fprintln(os.Stderr, "  dry-run  Print the computed start plan and exit")
	fmt.Fprintln(os.Stderr, "  check    Validate all unit files and print any errors")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Environment:")
	fmt.Fprintln(os.Stderr, "  NURA_UNIT_DIR  Directory with *.toml unit files (default: /etc/nura/services)")
}
