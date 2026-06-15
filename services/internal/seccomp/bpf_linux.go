//go:build linux

package seccomp

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Linux architecture constant for x86-64 (EM_X86_64 | __AUDIT_ARCH_64BIT | __AUDIT_ARCH_LE).
const auditArchAMD64 = 0xc000003e

// struct seccomp_data field offsets (bytes).
const (
	offsetNr   = 0 // syscall number (uint32)
	offsetArch = 4 // architecture  (uint32)
)

// BPF opcodes.
const (
	bpfLd  uint16 = 0x00
	bpfW   uint16 = 0x00
	bpfAbs uint16 = 0x20
	bpfJmp uint16 = 0x05
	bpfJeq uint16 = 0x10
	bpfK   uint16 = 0x00
	bpfRet uint16 = 0x06
)

// SECCOMP action codes.
const (
	retAllow uint32 = 0x7fff0000
	retLog   uint32 = 0x7ffc0000 // log and allow; requires kernel >= 4.14
	retErrno uint32 = 0x00050000
	retDeny         = retErrno | 1 // EPERM

	retKillProcess uint32 = 0x80000000
)

// prctl constants not in syscall package.
const (
	prSetNoNewPrivs  = 38
	prSetSeccomp     = 22
	seccompModeFilter = 2
)

func bpfStmt(code uint16, k uint32) syscall.SockFilter {
	return syscall.SockFilter{Code: code, K: k}
}

func bpfJump(code uint16, k uint32, jt, jf uint8) syscall.SockFilter {
	return syscall.SockFilter{Code: code, Jt: jt, Jf: jf, K: k}
}

// buildFilter constructs a BPF allowlist program.
// Architecture is checked first; unrecognised arches are killed.
// Each listed syscall returns ALLOW; all others use defaultAction.
func buildFilter(nrs []uint32, defaultAction uint32) []syscall.SockFilter {
	f := []syscall.SockFilter{
		// [0] load arch field
		bpfStmt(bpfLd|bpfW|bpfAbs, offsetArch),
		// [1] if arch == AUDIT_ARCH_X86_64 jump 1 else 0
		bpfJump(bpfJmp|bpfJeq|bpfK, auditArchAMD64, 1, 0),
		// [2] wrong arch -> kill process
		bpfStmt(bpfRet|bpfK, retKillProcess),
		// [3] load syscall number
		bpfStmt(bpfLd|bpfW|bpfAbs, offsetNr),
	}

	n := len(nrs)
	for i, nr := range nrs {
		// If this syscall matches, jump forward to the ALLOW instruction.
		// ALLOW is at absolute index 4+n+1; the true-branch offset is relative to
		// the NEXT instruction (4+i+1), so jt = (4+n+1) - (4+i+1) = n - i.
		jt := uint8(n - i)
		f = append(f, bpfJump(bpfJmp|bpfJeq|bpfK, nr, jt, 0))
	}

	// Default: deny (or log).
	f = append(f, bpfStmt(bpfRet|bpfK, defaultAction))
	// ALLOW target.
	f = append(f, bpfStmt(bpfRet|bpfK, retAllow))

	return f
}

// Apply installs the profile as a seccomp BPF filter on the calling process.
// The filter is inherited across fork and exec.
//
// PR_SET_NO_NEW_PRIVS is set unconditionally before the filter so that Apply
// works for both privileged (root/CAP_SYS_ADMIN) and unprivileged callers.
func Apply(p *Profile, mode Mode) error {
	if p == nil {
		return nil
	}
	if p.Mode != "" {
		mode = p.Mode
	}

	var defaultAction uint32
	switch mode {
	case ModeLog:
		defaultAction = retLog
	default:
		defaultAction = retDeny
	}

	nrs, unknown := resolveNames(p.Syscalls)
	if len(unknown) > 0 {
		return fmt.Errorf("seccomp: unknown syscall names: %v", unknown)
	}

	filter := buildFilter(nrs, defaultAction)
	if len(filter) > 65535 {
		return fmt.Errorf("seccomp: filter too large (%d instructions)", len(filter))
	}

	// PR_SET_NO_NEW_PRIVS allows unprivileged processes to install seccomp
	// filters without CAP_SYS_ADMIN. Setting it is always safe and required
	// when the caller lacks that capability.
	if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0); errno != 0 {
		return fmt.Errorf("prctl(PR_SET_NO_NEW_PRIVS): %w", errno)
	}

	prog := syscall.SockFprog{
		Len:    uint16(len(filter)),
		Filter: &filter[0],
	}

	if _, _, errno := syscall.RawSyscall(
		syscall.SYS_PRCTL,
		prSetSeccomp,
		seccompModeFilter,
		uintptr(unsafe.Pointer(&prog)),
	); errno != 0 {
		return fmt.Errorf("prctl(PR_SET_SECCOMP): %w", errno)
	}
	return nil
}

// resolveNames converts syscall names to numbers using the platform map.
// Returns the resolved numbers and any names not found in the map.
func resolveNames(names []string) (nrs []uint32, unknown []string) {
	for _, name := range names {
		nr, ok := syscallNumbers[name]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		nrs = append(nrs, uint32(nr))
	}
	return
}
