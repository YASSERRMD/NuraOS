package crashcap

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ReadResourceSnapshot reads lightweight resource accounting for the given PID
// from /proc. Returns a zero-value snapshot if /proc is not available.
func ReadResourceSnapshot(pid int) ResourceSnapshot {
	var snap ResourceSnapshot
	if pid <= 0 {
		return snap
	}

	// Memory: parse VmRSS from /proc/<pid>/status.
	statusPath := fmt.Sprintf("/proc/%d/status", pid)
	if f, err := os.Open(statusPath); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "VmRSS:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
						snap.MemUsageBytes = kb * 1024
					}
				}
				break
			}
		}
	}

	// Cgroup slice: read /proc/<pid>/cgroup.
	cgroupPath := fmt.Sprintf("/proc/%d/cgroup", pid)
	if data, err := os.ReadFile(cgroupPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			// cgroup v2 has a single line: "0::/..."
			if strings.HasPrefix(line, "0::") {
				snap.CgroupSlice = strings.TrimPrefix(line, "0::")
				break
			}
		}
	}

	// Open FD count: count entries in /proc/<pid>/fd.
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	if entries, err := os.ReadDir(fdDir); err == nil {
		snap.OpenFDs = len(entries)
	}

	return snap
}

// ReadLogTail reads the last n lines from logPath, redacting secrets.
// Returns an empty slice if the file does not exist or cannot be read.
func ReadLogTail(logPath string, n int) []string {
	data, err := os.ReadFile(filepath.Clean(logPath))
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
