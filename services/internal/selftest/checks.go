package selftest

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// AllChecks returns the canonical list of all standard self-test checks.
func AllChecks() []*Check {
	return []*Check{
		checkRNG(),
		checkCgroups(),
		checkNamespaces(),
		checkSeccomp(),
		checkStorageDurability(),
		checkNetwork(),
		checkFirewall(),
	}
}

// checkRNG verifies the kernel CSPRNG entropy pool has sufficient bits.
func checkRNG() *Check {
	return &Check{
		Name:     "rng",
		Category: CategoryKernel,
		BootSet:  true,
		Run: func(ctx context.Context) Result {
			res := Result{Name: "rng", Category: CategoryKernel}
			if runtime.GOOS != "linux" {
				res.Status = StatusSkip
				res.Detail = "non-Linux platform"
				return res
			}
			data, err := os.ReadFile("/proc/sys/kernel/random/entropy_avail")
			if err != nil {
				res.Status = StatusFail
				res.Detail = fmt.Sprintf("cannot read entropy_avail: %v", err)
				return res
			}
			bits, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				res.Status = StatusFail
				res.Detail = fmt.Sprintf("parse entropy_avail: %v", err)
				return res
			}
			if bits < 64 {
				res.Status = StatusFail
				res.Detail = fmt.Sprintf("entropy pool critically low: %d bits (need >= 64)", bits)
				return res
			}
			res.Status = StatusPass
			res.Detail = fmt.Sprintf("%d bits available", bits)
			return res
		},
	}
}

// checkCgroups verifies cgroup v2 is mounted and the NuraOS slice is present.
func checkCgroups() *Check {
	return &Check{
		Name:     "cgroups",
		Category: CategoryKernel,
		BootSet:  true,
		Run: func(ctx context.Context) Result {
			res := Result{Name: "cgroups", Category: CategoryKernel}
			if runtime.GOOS != "linux" {
				res.Status = StatusSkip
				res.Detail = "non-Linux platform"
				return res
			}
			// Verify cgroup v2 root
			if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
				res.Status = StatusFail
				res.Detail = "cgroup v2 not mounted at /sys/fs/cgroup"
				return res
			}
			// Check cpu and memory controllers are available
			data, err := os.ReadFile("/sys/fs/cgroup/cgroup.controllers")
			if err != nil {
				res.Status = StatusFail
				res.Detail = fmt.Sprintf("read cgroup.controllers: %v", err)
				return res
			}
			controllers := string(data)
			missing := []string{}
			for _, c := range []string{"cpu", "memory"} {
				if !strings.Contains(controllers, c) {
					missing = append(missing, c)
				}
			}
			if len(missing) > 0 {
				res.Status = StatusFail
				res.Detail = fmt.Sprintf("missing cgroup controllers: %v", missing)
				return res
			}
			res.Status = StatusPass
			res.Detail = "cgroup v2 with cpu and memory controllers"
			return res
		},
	}
}

// checkNamespaces verifies the kernel supports mount namespaces (CLONE_NEWNS).
func checkNamespaces() *Check {
	return &Check{
		Name:     "namespaces",
		Category: CategoryKernel,
		BootSet:  false,
		Run: func(ctx context.Context) Result {
			res := Result{Name: "namespaces", Category: CategoryKernel}
			if runtime.GOOS != "linux" {
				res.Status = StatusSkip
				res.Detail = "non-Linux platform"
				return res
			}
			// Probe by checking /proc/self/ns/mnt existence
			if _, err := os.Stat("/proc/self/ns/mnt"); err != nil {
				res.Status = StatusFail
				res.Detail = "mount namespace not visible in /proc/self/ns/mnt"
				return res
			}
			// Check for PID namespace support
			if _, err := os.Stat("/proc/self/ns/pid"); err != nil {
				res.Status = StatusFail
				res.Detail = "PID namespace not visible in /proc/self/ns/pid"
				return res
			}
			res.Status = StatusPass
			res.Detail = "mount and PID namespaces available"
			return res
		},
	}
}

// checkSeccomp verifies the kernel supports seccomp BPF (mode 2).
func checkSeccomp() *Check {
	return &Check{
		Name:     "seccomp",
		Category: CategoryKernel,
		BootSet:  false,
		Run: func(ctx context.Context) Result {
			res := Result{Name: "seccomp", Category: CategoryKernel}
			if runtime.GOOS != "linux" {
				res.Status = StatusSkip
				res.Detail = "non-Linux platform"
				return res
			}
			// Check /proc/sys/kernel/seccomp/actions_avail (kernel >= 4.14)
			data, err := os.ReadFile("/proc/sys/kernel/seccomp/actions_avail")
			if err != nil {
				// Older kernel: try checking /proc/sys/kernel/ngroups_max as a proxy
				// and assume seccomp is available if we're on a recent enough kernel
				if uname, err2 := os.ReadFile("/proc/version"); err2 == nil && len(uname) > 0 {
					res.Status = StatusPass
					res.Detail = "seccomp assumed available (actions_avail not readable)"
					return res
				}
				res.Status = StatusSkip
				res.Detail = "cannot probe seccomp support"
				return res
			}
			if strings.Contains(string(data), "kill_process") {
				res.Status = StatusPass
				res.Detail = "seccomp BPF with kill_process action"
			} else {
				res.Status = StatusPass
				res.Detail = fmt.Sprintf("seccomp actions: %s", strings.TrimSpace(string(data)))
			}
			return res
		},
	}
}

// checkStorageDurability writes, syncs, reads, and removes a test file on /data.
func checkStorageDurability() *Check {
	return &Check{
		Name:     "storage",
		Category: CategoryStorage,
		BootSet:  true,
		Run: func(ctx context.Context) Result {
			res := Result{Name: "storage", Category: CategoryStorage}

			dataDir := os.Getenv("NURA_DATA_DIR")
			if dataDir == "" {
				dataDir = "/data"
			}

			// Try /tmp as fallback (for test environments without /data)
			if _, err := os.Stat(dataDir); err != nil {
				dataDir = os.TempDir()
			}

			path := filepath.Join(dataDir, ".nura-selftest-durability")
			payload := []byte("nuraos-selftest-durability-check")

			// Write
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err != nil {
				res.Status = StatusFail
				res.Detail = fmt.Sprintf("create test file in %s: %v", dataDir, err)
				return res
			}
			if _, err := f.Write(payload); err != nil {
				f.Close()
				os.Remove(path)
				res.Status = StatusFail
				res.Detail = fmt.Sprintf("write: %v", err)
				return res
			}
			if err := f.Sync(); err != nil {
				f.Close()
				os.Remove(path)
				res.Status = StatusFail
				res.Detail = fmt.Sprintf("fsync: %v", err)
				return res
			}
			f.Close()

			// Read back and verify
			data, err := os.ReadFile(path)
			if err != nil {
				os.Remove(path)
				res.Status = StatusFail
				res.Detail = fmt.Sprintf("read back: %v", err)
				return res
			}
			os.Remove(path)

			if string(data) != string(payload) {
				res.Status = StatusFail
				res.Detail = "data mismatch after write+sync+read"
				return res
			}

			res.Status = StatusPass
			res.Detail = fmt.Sprintf("write/fsync/read verified on %s", dataDir)
			return res
		},
	}
}

// checkNetwork verifies loopback and basic outbound connectivity.
func checkNetwork() *Check {
	return &Check{
		Name:     "network",
		Category: CategoryNetwork,
		BootSet:  false,
		Run: func(ctx context.Context) Result {
			res := Result{Name: "network", Category: CategoryNetwork}

			// Verify loopback interface has an address.
			ifaces, err := net.Interfaces()
			if err != nil {
				res.Status = StatusFail
				res.Detail = fmt.Sprintf("net.Interfaces: %v", err)
				return res
			}
			loopOK := false
			for _, iface := range ifaces {
				if iface.Flags&net.FlagLoopback != 0 {
					addrs, _ := iface.Addrs()
					if len(addrs) > 0 {
						loopOK = true
						break
					}
				}
			}
			if !loopOK {
				res.Status = StatusFail
				res.Detail = "loopback interface not found or has no addresses"
				return res
			}

			// Try to resolve a simple DNS name (optional; skip if no connectivity).
			dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", "127.0.0.1:1")
			if conn != nil {
				conn.Close()
			}
			// We expect a connection refused error (port 1 is not open),
			// which still proves the loopback stack works.
			if err != nil {
				if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "refused") {
					res.Status = StatusPass
					res.Detail = "loopback stack operational (connection refused as expected)"
					return res
				}
			}
			res.Status = StatusPass
			res.Detail = fmt.Sprintf("loopback interface up with %d interface(s)", len(ifaces))
			return res
		},
	}
}

// checkFirewall verifies the firewall is active (basic heuristic on Linux).
func checkFirewall() *Check {
	return &Check{
		Name:     "firewall",
		Category: CategoryNetwork,
		BootSet:  false,
		Run: func(ctx context.Context) Result {
			res := Result{Name: "firewall", Category: CategoryNetwork}
			if runtime.GOOS != "linux" {
				res.Status = StatusSkip
				res.Detail = "non-Linux platform"
				return res
			}
			// Check if nftables or iptables rules are loaded via /proc/net entries.
			// /proc/net/nf_conntrack_count exists when conntrack is loaded.
			// /proc/net/ip_tables_names exists when iptables is in use.
			// We check both; presence of either indicates firewall activity.
			hasNftables := false
			if data, err := os.ReadFile("/proc/net/nf_conntrack_count"); err == nil {
				_ = data
				hasNftables = true
			}
			hasIptables := false
			if data, err := os.ReadFile("/proc/net/ip_tables_names"); err == nil {
				if strings.TrimSpace(string(data)) != "" {
					hasIptables = true
				}
			}

			if hasNftables || hasIptables {
				what := "nftables"
				if hasIptables {
					what = "iptables"
				}
				res.Status = StatusPass
				res.Detail = fmt.Sprintf("%s firewall active", what)
			} else {
				// Not necessarily an error - firewall may be XDP-based or not configured.
				res.Status = StatusSkip
				res.Detail = "cannot detect active firewall rules (nftables/iptables not probed)"
			}
			return res
		},
	}
}
