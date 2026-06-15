// Package cap manages Linux process capabilities.
//
// NuraOS uses capability bounding set trimming to ensure that services and
// their descendants can never gain capabilities they do not need, regardless
// of file capabilities or setuid bits on binaries they might exec.
//
// On non-Linux platforms all operations are no-ops.
package cap

import (
	"fmt"
	"strings"
)

// Drop removes each capability name in the list from the calling process's
// bounding set via prctl(PR_CAPBSET_DROP). The special value "all" removes
// every capability except cap_setuid and cap_setgid, which are retained for
// the su privilege-drop chain (su requires CAP_SETUID to call setresuid).
//
// The bounding set restriction is inherited across fork and exec: once dropped,
// a capability cannot be re-added regardless of file capabilities or setuid bits.
//
// Requires CAP_SETPCAP in the caller's effective set (available when running as root).
func Drop(names []string) error {
	expanded := expandAll(names)
	return dropBounding(expanded)
}

// Validate checks that all names are known capability identifiers without
// making any system call. Useful for early config validation.
func Validate(names []string) error {
	for _, n := range expandAll(names) {
		if _, ok := capabilityNumbers[strings.ToLower(n)]; !ok {
			return fmt.Errorf("cap: unknown capability %q", n)
		}
	}
	return nil
}

// expandAll replaces the special value "all" with the full list of capabilities
// minus cap_setuid and cap_setgid (which the su chain requires).
func expandAll(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if strings.ToLower(n) == "all" {
			for cap := range capabilityNumbers {
				if cap == "cap_setuid" || cap == "cap_setgid" {
					continue
				}
				out = append(out, cap)
			}
		} else {
			out = append(out, strings.ToLower(n))
		}
	}
	return out
}

// capabilityNumbers maps lowercase Linux capability names to their kernel numbers.
// Identical across all architectures for capability names known as of kernel 5.13.
var capabilityNumbers = map[string]uintptr{
	"cap_chown":              0,
	"cap_dac_override":       1,
	"cap_dac_read_search":    2,
	"cap_fowner":             3,
	"cap_fsetid":             4,
	"cap_kill":               5,
	"cap_setgid":             6,
	"cap_setuid":             7,
	"cap_setpcap":            8,
	"cap_linux_immutable":    9,
	"cap_net_bind_service":   10,
	"cap_net_broadcast":      11,
	"cap_net_admin":          12,
	"cap_net_raw":            13,
	"cap_ipc_lock":           14,
	"cap_ipc_owner":          15,
	"cap_sys_module":         16,
	"cap_sys_rawio":          17,
	"cap_sys_chroot":         18,
	"cap_sys_ptrace":         19,
	"cap_sys_pacct":          20,
	"cap_sys_admin":          21,
	"cap_sys_boot":           22,
	"cap_sys_nice":           23,
	"cap_sys_resource":       24,
	"cap_sys_time":           25,
	"cap_sys_tty_config":     26,
	"cap_mknod":              27,
	"cap_lease":              28,
	"cap_audit_write":        29,
	"cap_audit_control":      30,
	"cap_setfcap":            31,
	"cap_mac_override":       32,
	"cap_mac_admin":          33,
	"cap_syslog":             34,
	"cap_wake_alarm":         35,
	"cap_block_suspend":      36,
	"cap_audit_read":         37,
	"cap_perfmon":            38,
	"cap_bpf":                39,
	"cap_checkpoint_restore": 40,
}
