// Package toolsandbox runs state-changing tools inside an OS-enforced sandbox.
//
// The sandbox uses Linux kernel mechanisms (namespaces, Landlock, seccomp, cgroups)
// to enforce a capability-style grant model: a tool receives exactly the filesystem
// paths and syscalls it declares, nothing more. Even a buggy or malicious tool cannot
// exceed its declared scope because enforcement is in the kernel, not application code.
//
// Architecture: self-re-exec trampoline
//
// The running binary (nura-manager, test binary, etc.) is re-exec'd as the tool
// launcher when NURA_SANDBOX_APPLY=1. The trampoline applies Landlock, seccomp,
// and capability drops to itself, then exec's the actual tool. The parent:
//   1. Creates the per-tool cgroup for resource limits.
//   2. Starts the trampoline with namespace isolation flags.
//   3. Moves the trampoline PID into the cgroup.
//   4. Waits for the tool to finish.
//
// On non-Linux platforms the Run method returns ErrNotSupported.
package toolsandbox

import (
	"errors"
	"time"
)

// ErrNotSupported is returned by Run on non-Linux platforms.
var ErrNotSupported = errors.New("toolsandbox: OS-level sandbox requires Linux")

// Grant declares the kernel-enforced permissions for one tool invocation.
// Only the rights declared here can be used by the tool; all others are denied
// by the kernel before the tool's code ever runs.
type Grant struct {
	// Name is a human-readable tool identifier used for logging and cgroup naming.
	Name string `json:"name"`
	// Paths is the filesystem access declaration enforced by Landlock.
	// Paths not listed here are invisible to the tool (EACCES on access).
	// An empty list means no path restrictions (Landlock not applied).
	Paths []PathGrant `json:"paths,omitempty"`
	// Syscalls is the seccomp allowlist of syscall names.
	// An empty list means no syscall restrictions (seccomp not applied).
	Syscalls []string `json:"syscalls,omitempty"`
	// MemLimit is the cgroup memory.max in bytes. 0 = unlimited.
	MemLimit uint64 `json:"mem_limit,omitempty"`
	// CPUWeight is the cgroup cpu.weight (1-10000). 0 = use default (100).
	CPUWeight int `json:"cpu_weight,omitempty"`
	// Timeout is the wall-clock timeout for the tool. 0 = no limit.
	Timeout time.Duration `json:"timeout_ns,omitempty"`
}

// PathGrant allows specific filesystem access to one path (and everything beneath it).
type PathGrant struct {
	// Path is the directory or file to grant access to.
	Path string `json:"path"`
	// Read allows reading file contents and directory listings.
	Read bool `json:"read,omitempty"`
	// Write allows creating, modifying, and deleting files and directories.
	Write bool `json:"write,omitempty"`
	// Exec allows executing files beneath this path.
	Exec bool `json:"exec,omitempty"`
}

// Result is the outcome of a sandboxed tool execution.
type Result struct {
	// ExitCode is the tool's exit status. -1 if the process was killed.
	ExitCode int
	// Stdout captures the tool's standard output.
	Stdout []byte
	// Stderr captures the tool's standard error.
	Stderr []byte
	// Killed is true when the tool was terminated by the timeout or OOM killer.
	Killed bool
}
