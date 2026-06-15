package journal

import (
	"os"
	"strconv"
	"strings"
)

// openKmsg opens the kernel message device for reading. On non-Linux or when
// the device is absent it returns a non-nil error so CollectKmsg can skip.
func openKmsg(path string) (*os.File, error) {
	if path == "" {
		path = "/dev/kmsg"
	}
	return os.Open(path)
}

// parseKmsgLine parses one /dev/kmsg record and returns (priority, message).
// Format: "<syslog_pri+facility>,<seq>,<ts_usec>,<flags>;<message>"
// The syslog priority is the low 3 bits of the first field.
func parseKmsgLine(line string) (Priority, string) {
	parts := strings.SplitN(line, ";", 2)
	msg := line
	if len(parts) == 2 {
		msg = parts[1]
	}

	// Extract syslog level from the first comma-separated field.
	pri := PriInfo
	if len(parts) == 2 {
		fields := strings.SplitN(parts[0], ",", 2)
		if len(fields) >= 1 {
			if n, err := strconv.Atoi(fields[0]); err == nil {
				level := n & 0x7
				if level >= 0 && level <= 7 {
					pri = Priority(level)
				}
			}
		}
	}
	return pri, msg
}
