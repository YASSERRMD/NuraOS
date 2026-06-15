//go:build linux

package toolsandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"

	"github.com/yasserrmd/nuraos/services/internal/cap"
	"github.com/yasserrmd/nuraos/services/internal/landlock"
	"github.com/yasserrmd/nuraos/services/internal/seccomp"
)

const (
	// envApply signals the binary is running as a sandbox trampoline.
	envApply = "NURA_SANDBOX_APPLY"
	// envGrant carries the JSON-encoded Grant.
	envGrant = "NURA_SANDBOX_GRANT"
)

// MaybeApplyAndExec checks for NURA_SANDBOX_APPLY=1. When set it:
//  1. Parses the Grant from NURA_SANDBOX_GRANT.
//  2. Applies Landlock filesystem confinement.
//  3. Applies seccomp syscall allowlist.
//  4. Drops all capabilities.
//  5. Exec's the tool found in os.Args after "--".
//
// This function never returns when NURA_SANDBOX_APPLY=1; it either exec's the
// tool or calls os.Exit(126) on error. The caller must invoke this at the very
// start of main() (or TestMain) before any I/O or resource acquisition.
//
// When NURA_SANDBOX_APPLY is not set, MaybeApplyAndExec returns false immediately.
func MaybeApplyAndExec() bool {
	if os.Getenv(envApply) != "1" {
		return false
	}

	grantData := os.Getenv(envGrant)
	var g Grant
	if grantData != "" {
		if err := json.Unmarshal([]byte(grantData), &g); err != nil {
			fmt.Fprintf(os.Stderr, "toolsandbox: malformed grant JSON: %v\n", err)
			os.Exit(126)
		}
	}

	// Locate "--" separator in argv to find the actual tool path and args.
	sepIdx := -1
	for i, a := range os.Args {
		if a == "--" {
			sepIdx = i
			break
		}
	}
	if sepIdx < 0 || sepIdx+1 >= len(os.Args) {
		fmt.Fprintln(os.Stderr, "toolsandbox: argv missing -- separator")
		os.Exit(126)
	}
	toolPath := os.Args[sepIdx+1]
	toolArgs := os.Args[sepIdx+1:]

	// Apply Landlock filesystem confinement.
	if len(g.Paths) > 0 {
		ll := buildLandlockProfile(g)
		if err := landlock.Apply(ll); err != nil {
			// Non-fatal: older kernels don't support Landlock.
			fmt.Fprintf(os.Stderr, "toolsandbox: landlock skipped: %v\n", err)
		}
	}

	// Apply seccomp syscall allowlist.
	if len(g.Syscalls) > 0 {
		p := &seccomp.Profile{Syscalls: g.Syscalls}
		if err := seccomp.Apply(p, seccomp.ModeEnforce); err != nil {
			fmt.Fprintf(os.Stderr, "toolsandbox: seccomp failed: %v\n", err)
			os.Exit(126)
		}
	}

	// Drop all Linux capabilities from the bounding set.
	if err := cap.Drop([]string{"all"}); err != nil {
		fmt.Fprintf(os.Stderr, "toolsandbox: cap drop failed: %v\n", err)
		// Non-fatal: cap drop may fail without CAP_SETPCAP.
	}

	// Clean the environment: remove sandbox control variables before exec.
	env := cleanEnv()

	// Exec the actual tool. This replaces the current process image.
	if err := syscall.Exec(toolPath, toolArgs, env); err != nil {
		fmt.Fprintf(os.Stderr, "toolsandbox: exec %q failed: %v\n", toolPath, err)
		os.Exit(126)
	}
	return true // never reached; exec replaced the process
}

// buildLandlockProfile converts Grant.Paths into a landlock.Profile.
func buildLandlockProfile(g Grant) *landlock.Profile {
	var rules []landlock.PathRule
	for _, pg := range g.Paths {
		var acc []landlock.Access
		if pg.Read {
			acc = append(acc, landlock.AccessReadFile, landlock.AccessReadDir)
		}
		if pg.Write {
			acc = append(acc,
				landlock.AccessWriteFile,
				landlock.AccessMakeReg,
				landlock.AccessMakeDir,
				landlock.AccessRemoveFile,
				landlock.AccessRemoveDir,
			)
		}
		if pg.Exec {
			acc = append(acc, landlock.AccessExecute)
		}
		if len(acc) > 0 {
			rules = append(rules, landlock.PathRule{Path: pg.Path, Access: acc})
		}
	}
	return &landlock.Profile{Paths: rules}
}

// cleanEnv returns os.Environ() with sandbox control variables stripped.
func cleanEnv() []string {
	strip := map[string]bool{envApply: true, envGrant: true}
	var out []string
	for _, kv := range os.Environ() {
		key := kv
		for i, c := range kv {
			if c == '=' {
				key = kv[:i]
				break
			}
		}
		if !strip[key] {
			out = append(out, kv)
		}
	}
	return out
}
