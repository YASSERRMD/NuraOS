package identity

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// SysInfo holds the system identity and runtime state.
// It is safe to marshal to JSON; the machine_id field is non-sensitive.
type SysInfo struct {
	MachineID string  `json:"machine_id"`
	Hostname  string  `json:"hostname"`
	OSVersion string  `json:"os_version"`
	Model     string  `json:"model"`
	UptimeSec float64 `json:"uptime_sec"`
}

// Gather collects the current system identity. startTime is used as an uptime
// fallback on platforms where /proc/uptime is absent.
func Gather(machineID, hostname string, startTime time.Time) SysInfo {
	return SysInfo{
		MachineID: machineID,
		Hostname:  hostname,
		OSVersion: OSVersion,
		Model:     readModel(),
		UptimeSec: readUptime(startTime),
	}
}

// FormatStatus returns a compact one-line description for log output.
func (s SysInfo) FormatStatus() string {
	return fmt.Sprintf("hostname=%s machine_id=%s model=%s uptime=%.0fs version=%s",
		s.Hostname, s.MachineID, s.Model, s.UptimeSec, s.OSVersion)
}

func readModel() string {
	for _, path := range []string{
		"/sys/class/dmi/id/product_name",
		"/proc/device-tree/model",
	} {
		if data, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(strings.TrimRight(string(data), "\x00"))
		}
	}
	return "unknown"
}

func readUptime(startTime time.Time) float64 {
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		if fields := strings.Fields(string(data)); len(fields) > 0 {
			if f, err := strconv.ParseFloat(fields[0], 64); err == nil {
				return f
			}
		}
	}
	return time.Since(startTime).Seconds()
}
