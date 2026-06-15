// Package unit defines the NuraOS service unit model.
//
// Each service is described by a TOML file under /etc/nura/services/.
// Units declare their executable, dependencies, restart policy, resource
// limits, and readiness probe so the service manager can orchestrate them.
package unit

// Type describes how the service signals readiness.
type Type string

const (
	// TypeOneshot runs once and is considered done when it exits 0.
	TypeOneshot Type = "oneshot"
	// TypeLongrun runs indefinitely; readiness is probed separately.
	TypeLongrun Type = "longrun"
	// TypeNotify like longrun but the process signals ready via the notify fd.
	TypeNotify Type = "notify"
)

// RestartPolicy controls when the service manager restarts a unit.
type RestartPolicy string

const (
	RestartNo        RestartPolicy = "no"
	RestartOnFailure RestartPolicy = "on-failure"
	RestartAlways    RestartPolicy = "always"
)

// ReadinessType specifies how readiness is detected.
type ReadinessType string

const (
	ReadinessHTTP   ReadinessType = "http"
	ReadinessSocket ReadinessType = "socket"
	ReadinessNone   ReadinessType = "none"
)

// Restart holds crash-recovery settings for a unit.
type Restart struct {
	Policy      RestartPolicy `toml:"policy"`
	MaxRestarts int           `toml:"max_restarts"`
	BackoffInit int           `toml:"backoff_initial"`
	BackoffMax  int           `toml:"backoff_max"`
	// CrashLoopLimit is the number of crashes within CrashLoopWindow seconds
	// that triggers the crash-loop breaker.
	CrashLoopLimit  int `toml:"crash_loop_limit"`
	CrashLoopWindow int `toml:"crash_loop_window"`
	// CrashLoopBackoff is how long (seconds) to pause after a crash loop.
	CrashLoopBackoff int `toml:"crash_loop_backoff"`
}

// Resources specifies optional cgroup-style limits applied by the manager.
// Enforcement requires cgroup v2 (Phase 71); values are stored here for
// forward compatibility.
type Resources struct {
	CPUWeight int    `toml:"cpu_weight"`
	MemoryMax string `toml:"memory_max"`
	IOWeight  int    `toml:"io_weight"`
}

// Readiness describes how to detect that the service is ready to accept work.
type Readiness struct {
	Type    ReadinessType `toml:"type"`
	URL     string        `toml:"url"`
	Socket  string        `toml:"socket"`
	Timeout int           `toml:"timeout"`
}

// SocketActivation configures lazy-start socket activation for a unit.
// When enabled the manager pre-opens the listen socket; the service is started
// only when the first client connects. The socket fd is passed to the service
// as LISTEN_FDS=1 / LISTEN_PID=<pid>. An optional idle_timeout stops the
// service when there has been no connection activity for that many seconds.
type SocketActivation struct {
	Enabled     bool   `toml:"enabled"`
	Network     string `toml:"network"`      // "tcp" or "unix"
	Address     string `toml:"address"`      // bind address
	IdleTimeout int    `toml:"idle_timeout"` // 0 = no idle stop
}

// Unit is the in-memory representation of a parsed service unit file.
type Unit struct {
	// Name is the canonical service identifier (must be unique).
	Name string `toml:"name"`
	// Description is a human-readable summary.
	Description string `toml:"description"`
	// Exec is the command the manager runs (absolute path + args).
	Exec string `toml:"exec"`
	// Args are optional additional arguments appended to Exec.
	Args []string `toml:"args"`
	// Type controls readiness signalling semantics.
	Type Type `toml:"type"`
	// User is the UNIX account the process runs as (default: root).
	User string `toml:"user"`
	// After lists units that must be started before this one (ordering only).
	After []string `toml:"after"`
	// Requires lists units that must be started AND healthy before this one.
	Requires []string `toml:"requires"`
	// Restart holds crash-recovery configuration.
	Restart Restart `toml:"restart"`
	// Resources holds optional resource limit declarations.
	Resources Resources `toml:"resources"`
	// Readiness describes the readiness probe.
	Readiness Readiness `toml:"readiness"`
	// SocketActivation configures lazy socket-activated start.
	SocketActivation SocketActivation `toml:"socket_activation"`
	// Enabled controls whether the unit participates in the start plan.
	Enabled bool `toml:"enabled"`
}

// defaults fills in sensible zero-value defaults after parsing.
func (u *Unit) defaults() {
	if u.Type == "" {
		u.Type = TypeLongrun
	}
	if u.Restart.Policy == "" {
		u.Restart.Policy = RestartOnFailure
	}
	if u.Restart.MaxRestarts == 0 {
		u.Restart.MaxRestarts = 5
	}
	if u.Restart.BackoffInit == 0 {
		u.Restart.BackoffInit = 1
	}
	if u.Restart.BackoffMax == 0 {
		u.Restart.BackoffMax = 30
	}
	if u.Restart.CrashLoopLimit == 0 {
		u.Restart.CrashLoopLimit = 5
	}
	if u.Restart.CrashLoopWindow == 0 {
		u.Restart.CrashLoopWindow = 60
	}
	if u.Restart.CrashLoopBackoff == 0 {
		u.Restart.CrashLoopBackoff = 120
	}
	if u.Readiness.Type == "" {
		u.Readiness.Type = ReadinessNone
	}
	if u.Readiness.Timeout == 0 {
		u.Readiness.Timeout = 30
	}
}
