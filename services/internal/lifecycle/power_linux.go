//go:build linux

package lifecycle

import "syscall"

// SysHalt powers off the system via the LINUX_REBOOT_CMD_POWER_OFF syscall.
// This call does not return on success.
func SysHalt() error { return syscall.Reboot(syscall.LINUX_REBOOT_CMD_POWER_OFF) }

// SysReboot reboots the system via the LINUX_REBOOT_CMD_RESTART syscall.
// This call does not return on success.
func SysReboot() error { return syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART) }
