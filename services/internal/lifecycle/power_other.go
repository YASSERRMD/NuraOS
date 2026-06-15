//go:build !linux

package lifecycle

import "fmt"

func SysHalt() error  { return fmt.Errorf("poweroff not supported on this platform") }
func SysReboot() error { return fmt.Errorf("reboot not supported on this platform") }
