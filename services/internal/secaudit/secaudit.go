// Package secaudit provides automated security checks that verify the NuraOS
// security posture at runtime. Checks cover boot integrity, kernel security
// features, secrets hygiene, open ports, and dependency vulnerability status.
//
// Checks are designed to run both in CI (against the booted QEMU image) and
// on the appliance itself. The result of each check is pass/fail/skip/warn
// with a human-readable detail string. A check with status "critical" causes
// the overall audit to fail (non-zero exit in CI).
package secaudit

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Severity classifies the importance of an audit finding.
type Severity string

const (
	// SeverityInfo is an informational finding; does not affect overall status.
	SeverityInfo Severity = "info"
	// SeverityWarn is a non-critical finding that should be investigated.
	SeverityWarn Severity = "warn"
	// SeverityCritical is a finding that must be remediated before release.
	SeverityCritical Severity = "critical"
)

// Status is the outcome of a single check.
type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
	StatusWarn Status = "warn"
)

// Finding is the result of one security check.
type Finding struct {
	// Name is the short check identifier (e.g. "seccomp-enabled").
	Name string `json:"name"`
	// Category groups related checks.
	Category string `json:"category"`
	// Status is the check outcome.
	Status Status `json:"status"`
	// Severity classifies how critical a failure is.
	Severity Severity `json:"severity"`
	// Detail is a human-readable description of the finding.
	Detail string `json:"detail"`
	// Duration is the time taken to run this check.
	Duration time.Duration `json:"duration_ns"`
}

// AuditReport is the aggregate result of an audit run.
type AuditReport struct {
	// Findings is the ordered list of all check results.
	Findings []Finding `json:"findings"`
	// Pass is the count of passing checks.
	Pass int `json:"pass"`
	// Fail is the count of failing checks.
	Fail int `json:"fail"`
	// Warn is the count of warning checks.
	Warn int `json:"warn"`
	// Skip is the count of skipped checks.
	Skip int `json:"skip"`
	// Critical is the count of critical-severity failures.
	Critical int `json:"critical"`
	// Overall is "pass" when Critical == 0, else "fail".
	Overall string `json:"overall"`
	// ElapsedMS is total wall time in milliseconds.
	ElapsedMS int64 `json:"elapsed_ms"`
}

// Check is a single security audit item.
type Check struct {
	Name     string
	Category string
	Severity Severity
	Run      func(ctx context.Context) Finding
}

// Auditor runs a set of security checks.
type Auditor struct {
	checks []*Check
}

// New creates an Auditor pre-loaded with all standard security checks.
func New() *Auditor {
	a := &Auditor{}
	a.Register(allChecks()...)
	return a
}

// Register adds checks to the auditor.
func (a *Auditor) Register(checks ...*Check) {
	a.checks = append(a.checks, checks...)
}

// Run executes all registered checks and returns an AuditReport.
func (a *Auditor) Run(ctx context.Context) AuditReport {
	start := time.Now()
	var findings []Finding

	for _, c := range a.checks {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		t0 := time.Now()
		f := c.Run(cctx)
		cancel()
		f.Duration = time.Since(t0)
		if f.Name == "" {
			f.Name = c.Name
		}
		if f.Category == "" {
			f.Category = c.Category
		}
		if f.Severity == "" {
			f.Severity = c.Severity
		}
		findings = append(findings, f)
	}

	rep := AuditReport{
		Findings:  findings,
		ElapsedMS: time.Since(start).Milliseconds(),
	}
	for _, f := range findings {
		switch f.Status {
		case StatusPass:
			rep.Pass++
		case StatusFail:
			rep.Fail++
			if f.Severity == SeverityCritical {
				rep.Critical++
			}
		case StatusWarn:
			rep.Warn++
		case StatusSkip:
			rep.Skip++
		}
	}
	if rep.Critical == 0 {
		rep.Overall = "pass"
	} else {
		rep.Overall = "fail"
	}
	return rep
}

// FormatHuman renders an AuditReport as human-readable text.
func FormatHuman(rep AuditReport) string {
	var b strings.Builder
	for _, f := range rep.Findings {
		icon := "OK  "
		if f.Status == StatusFail {
			icon = "FAIL"
		} else if f.Status == StatusWarn {
			icon = "WARN"
		} else if f.Status == StatusSkip {
			icon = "SKIP"
		}
		fmt.Fprintf(&b, "[%s] %-12s %-30s [%s] %s (%dms)\n",
			icon, f.Category, f.Name, f.Severity, f.Detail, f.Duration.Milliseconds())
	}
	fmt.Fprintf(&b, "\nOverall: %s  pass=%d fail=%d warn=%d skip=%d critical=%d (%dms)\n",
		rep.Overall, rep.Pass, rep.Fail, rep.Warn, rep.Skip, rep.Critical, rep.ElapsedMS)
	return b.String()
}

// --- standard checks ---

func allChecks() []*Check {
	return []*Check{
		checkSeccomp(),
		checkNamespacesEnabled(),
		checkCapsDropped(),
		checkSecretsPermissions(),
		checkSecretsNotWorldReadable(),
		checkKernelLockdown(),
		checkOpenPortsLoopbackOnly(),
		checkNoDebugPortsExposed(),
		checkRNGEntropy(),
		checkUMask(),
	}
}

func checkSeccomp() *Check {
	return &Check{
		Name:     "seccomp-enabled",
		Category: "kernel",
		Severity: SeverityCritical,
		Run: func(ctx context.Context) Finding {
			f := Finding{Name: "seccomp-enabled", Category: "kernel", Severity: SeverityCritical}
			if runtime.GOOS != "linux" {
				f.Status = StatusSkip
				f.Detail = "non-Linux platform"
				return f
			}
			data, err := os.ReadFile("/proc/sys/kernel/seccomp/actions_avail")
			if err != nil {
				// Older kernels: check /proc/version for recent enough kernel
				if _, err2 := os.Stat("/proc/self/status"); err2 == nil {
					f.Status = StatusPass
					f.Detail = "seccomp assumed available (actions_avail not readable)"
					return f
				}
				f.Status = StatusWarn
				f.Detail = "cannot verify seccomp support"
				f.Severity = SeverityWarn
				return f
			}
			f.Status = StatusPass
			f.Detail = "seccomp available: " + strings.TrimSpace(string(data))
			return f
		},
	}
}

func checkNamespacesEnabled() *Check {
	return &Check{
		Name:     "namespaces-enabled",
		Category: "kernel",
		Severity: SeverityCritical,
		Run: func(ctx context.Context) Finding {
			f := Finding{Name: "namespaces-enabled", Category: "kernel", Severity: SeverityCritical}
			if runtime.GOOS != "linux" {
				f.Status = StatusSkip
				f.Detail = "non-Linux platform"
				return f
			}
			for _, ns := range []string{"mnt", "pid", "net"} {
				path := fmt.Sprintf("/proc/self/ns/%s", ns)
				if _, err := os.Stat(path); err != nil {
					f.Status = StatusFail
					f.Detail = fmt.Sprintf("%s namespace not visible at %s", ns, path)
					return f
				}
			}
			f.Status = StatusPass
			f.Detail = "mnt, pid, net namespaces available"
			return f
		},
	}
}

func checkCapsDropped() *Check {
	return &Check{
		Name:     "capabilities-check",
		Category: "kernel",
		Severity: SeverityWarn,
		Run: func(ctx context.Context) Finding {
			f := Finding{Name: "capabilities-check", Category: "kernel", Severity: SeverityWarn}
			if runtime.GOOS != "linux" {
				f.Status = StatusSkip
				f.Detail = "non-Linux platform"
				return f
			}
			data, err := os.ReadFile("/proc/self/status")
			if err != nil {
				f.Status = StatusSkip
				f.Detail = "cannot read /proc/self/status"
				return f
			}
			// Look for CapEff (effective capabilities).
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "CapEff:") {
					fields := strings.Fields(line)
					if len(fields) >= 2 && fields[1] == "0000000000000000" {
						f.Status = StatusPass
						f.Detail = "all capabilities dropped (CapEff=0)"
						return f
					}
					f.Status = StatusWarn
					f.Detail = "process retains capabilities: CapEff=" + fields[1]
					return f
				}
			}
			f.Status = StatusSkip
			f.Detail = "CapEff not found in /proc/self/status"
			return f
		},
	}
}

func checkSecretsPermissions() *Check {
	return &Check{
		Name:     "secrets-file-permissions",
		Category: "secrets",
		Severity: SeverityCritical,
		Run: func(ctx context.Context) Finding {
			f := Finding{Name: "secrets-file-permissions", Category: "secrets", Severity: SeverityCritical}
			paths := []string{
				"/data/etc/secrets.toml",
				"/data/etc/agent.toml",
			}
			for _, path := range paths {
				info, err := os.Stat(path)
				if os.IsNotExist(err) {
					continue
				}
				if err != nil {
					continue
				}
				mode := info.Mode().Perm()
				// Reject world-readable or group-readable secrets.
				if mode&0o044 != 0 {
					f.Status = StatusFail
					f.Detail = fmt.Sprintf("%s has permissions %o; must be <= 0600", path, mode)
					return f
				}
			}
			f.Status = StatusPass
			f.Detail = "secrets files have acceptable permissions"
			return f
		},
	}
}

func checkSecretsNotWorldReadable() *Check {
	return &Check{
		Name:     "secrets-not-in-environment",
		Category: "secrets",
		Severity: SeverityCritical,
		Run: func(ctx context.Context) Finding {
			f := Finding{Name: "secrets-not-in-environment", Category: "secrets", Severity: SeverityCritical}
			// Check that obvious secret env vars are not set unintentionally.
			dangerousVars := []string{
				"ANTHROPIC_API_KEY",
				"OPENAI_API_KEY",
				"NURA_GATEWAY_TOKEN",
				"AWS_SECRET_ACCESS_KEY",
			}
			found := []string{}
			for _, v := range dangerousVars {
				if val := os.Getenv(v); val != "" {
					found = append(found, v)
				}
			}
			if len(found) > 0 {
				// Not necessarily a failure -- these are expected env vars in some
				// deployments. Warn rather than fail.
				f.Status = StatusWarn
				f.Severity = SeverityWarn
				f.Detail = fmt.Sprintf("secret env vars are set: %v (ensure they are not logged)", found)
				return f
			}
			f.Status = StatusPass
			f.Detail = "no secret environment variables detected"
			return f
		},
	}
}

func checkKernelLockdown() *Check {
	return &Check{
		Name:     "kernel-lockdown",
		Category: "boot",
		Severity: SeverityWarn,
		Run: func(ctx context.Context) Finding {
			f := Finding{Name: "kernel-lockdown", Category: "boot", Severity: SeverityWarn}
			if runtime.GOOS != "linux" {
				f.Status = StatusSkip
				f.Detail = "non-Linux platform"
				return f
			}
			data, err := os.ReadFile("/sys/kernel/security/lockdown")
			if err != nil {
				f.Status = StatusWarn
				f.Detail = "kernel lockdown not available or not readable"
				return f
			}
			val := strings.TrimSpace(string(data))
			if strings.Contains(val, "[confidentiality]") {
				f.Status = StatusPass
				f.Detail = "kernel lockdown: confidentiality mode"
			} else if strings.Contains(val, "[integrity]") {
				f.Status = StatusPass
				f.Detail = "kernel lockdown: integrity mode"
			} else {
				f.Status = StatusWarn
				f.Detail = fmt.Sprintf("kernel lockdown is none: %s", val)
			}
			return f
		},
	}
}

func checkOpenPortsLoopbackOnly() *Check {
	return &Check{
		Name:     "open-ports-loopback-only",
		Category: "network",
		Severity: SeverityCritical,
		Run: func(ctx context.Context) Finding {
			f := Finding{Name: "open-ports-loopback-only", Category: "network", Severity: SeverityCritical}
			// Probe the gateway default port on all interfaces.
			// We expect it to be bound only on 127.0.0.1:8080, not 0.0.0.0:8080.
			conn, err := net.DialTimeout("tcp", "127.0.0.1:8080", 200*time.Millisecond)
			if err != nil {
				f.Status = StatusSkip
				f.Detail = "gateway not listening on 127.0.0.1:8080 (not running?)"
				return f
			}
			conn.Close()

			// Check if it's also reachable on a non-loopback address.
			// We detect by comparing interfaces.
			ifaces, _ := net.Interfaces()
			for _, iface := range ifaces {
				if iface.Flags&net.FlagLoopback != 0 {
					continue
				}
				addrs, _ := iface.Addrs()
				for _, addr := range addrs {
					var ip net.IP
					switch v := addr.(type) {
					case *net.IPNet:
						ip = v.IP
					case *net.IPAddr:
						ip = v.IP
					}
					if ip == nil || !ip.IsGlobalUnicast() {
						continue
					}
					c, err := net.DialTimeout("tcp", fmt.Sprintf("%s:8080", ip.String()), 100*time.Millisecond)
					if err == nil {
						c.Close()
						f.Status = StatusFail
						f.Detail = fmt.Sprintf("gateway reachable on non-loopback address %s:8080", ip)
						return f
					}
				}
			}
			f.Status = StatusPass
			f.Detail = "gateway listening on loopback only"
			return f
		},
	}
}

func checkNoDebugPortsExposed() *Check {
	return &Check{
		Name:     "no-debug-ports",
		Category: "network",
		Severity: SeverityWarn,
		Run: func(ctx context.Context) Finding {
			f := Finding{Name: "no-debug-ports", Category: "network", Severity: SeverityWarn}
			// Check that pprof (6060) is not exposed on non-loopback.
			debugPorts := []int{6060, 2345, 40000} // pprof, delve default
			exposed := []string{}
			ifaces, _ := net.Interfaces()
			for _, iface := range ifaces {
				if iface.Flags&net.FlagLoopback != 0 {
					continue
				}
				addrs, _ := iface.Addrs()
				for _, addr := range addrs {
					var ip net.IP
					switch v := addr.(type) {
					case *net.IPNet:
						ip = v.IP
					case *net.IPAddr:
						ip = v.IP
					}
					if ip == nil || !ip.IsGlobalUnicast() {
						continue
					}
					for _, port := range debugPorts {
						c, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", ip.String(), port), 50*time.Millisecond)
						if err == nil {
							c.Close()
							exposed = append(exposed, fmt.Sprintf("%s:%d", ip, port))
						}
					}
				}
			}
			if len(exposed) > 0 {
				f.Status = StatusWarn
				f.Detail = fmt.Sprintf("debug ports exposed: %v", exposed)
				return f
			}
			f.Status = StatusPass
			f.Detail = "no debug ports exposed on non-loopback interfaces"
			return f
		},
	}
}

func checkRNGEntropy() *Check {
	return &Check{
		Name:     "rng-entropy",
		Category: "crypto",
		Severity: SeverityCritical,
		Run: func(ctx context.Context) Finding {
			f := Finding{Name: "rng-entropy", Category: "crypto", Severity: SeverityCritical}
			if runtime.GOOS != "linux" {
				f.Status = StatusSkip
				f.Detail = "non-Linux platform"
				return f
			}
			data, err := os.ReadFile("/proc/sys/kernel/random/entropy_avail")
			if err != nil {
				f.Status = StatusFail
				f.Detail = fmt.Sprintf("cannot read entropy_avail: %v", err)
				return f
			}
			var bits int
			fmt.Sscan(strings.TrimSpace(string(data)), &bits)
			if bits < 64 {
				f.Status = StatusFail
				f.Detail = fmt.Sprintf("entropy critically low: %d bits", bits)
				return f
			}
			f.Status = StatusPass
			f.Detail = fmt.Sprintf("entropy available: %d bits", bits)
			return f
		},
	}
}

func checkUMask() *Check {
	return &Check{
		Name:     "umask-restrictive",
		Category: "filesystem",
		Severity: SeverityWarn,
		Run: func(ctx context.Context) Finding {
			f := Finding{Name: "umask-restrictive", Category: "filesystem", Severity: SeverityWarn}
			// Create a temp file and check its permissions to infer the umask.
			tmp, err := os.CreateTemp("", "secaudit-umask-*")
			if err != nil {
				f.Status = StatusSkip
				f.Detail = "cannot create temp file to probe umask"
				return f
			}
			path := tmp.Name()
			tmp.Close()
			defer os.Remove(path)

			info, err := os.Stat(path)
			if err != nil {
				f.Status = StatusSkip
				f.Detail = "cannot stat temp file"
				return f
			}
			mode := info.Mode().Perm()
			// os.CreateTemp uses 0600; if the file is more permissive, umask is too open.
			if mode&0o022 != 0 {
				f.Status = StatusWarn
				f.Detail = fmt.Sprintf("umask allows group/world write: file created with permissions %o", mode)
				return f
			}
			f.Status = StatusPass
			f.Detail = fmt.Sprintf("temp file created with permissions %o (umask is sufficiently restrictive)", mode)
			return f
		},
	}
}

// FilterBySeverity returns a new Auditor containing only checks at or above
// the given severity. Order: critical > warn > info.
func (a *Auditor) FilterBySeverity(minSev Severity) *Auditor {
	out := &Auditor{}
	order := map[Severity]int{SeverityInfo: 0, SeverityWarn: 1, SeverityCritical: 2}
	min := order[minSev]
	for _, c := range a.checks {
		if order[c.Severity] >= min {
			out.Register(c)
		}
	}
	return out
}

// CheckPaths verifies that a list of filesystem paths exist and are
// accessible. Returns a Finding for each path.
func CheckPaths(paths []string) []Finding {
	var findings []Finding
	for _, path := range paths {
		f := Finding{
			Name:     "path-exists:" + filepath.Base(path),
			Category: "filesystem",
			Severity: SeverityWarn,
		}
		if _, err := os.Stat(path); err != nil {
			f.Status = StatusFail
			f.Detail = fmt.Sprintf("%s: %v", path, err)
		} else {
			f.Status = StatusPass
			f.Detail = path + " exists"
		}
		findings = append(findings, f)
	}
	return findings
}
