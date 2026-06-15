// Package ctlsock defines the JSON-over-Unix-socket protocol used between
// the nura-manager control server and the nuractl CLI.
//
// Authentication is enforced by the socket file permissions (mode 0660,
// owner root, group manager). Only processes with the appropriate gid can
// connect; no token exchange is needed.
//
// Each request is one JSON line; each response is one JSON line. The
// connection is closed after one request/response pair.
package ctlsock

const SocketPath = "/run/nura-manager.sock"

// Command names.
const (
	CmdList    = "list"
	CmdStatus  = "status"
	CmdStart   = "start"
	CmdStop    = "stop"
	CmdRestart = "restart"
	CmdLogs    = "logs"
	CmdEnable  = "enable"
	CmdDisable = "disable"
)

// Request is the JSON structure sent by nuractl to the manager.
type Request struct {
	Command string `json:"command"`
	Service string `json:"service,omitempty"`
	Follow  bool   `json:"follow,omitempty"`
	Lines   int    `json:"lines,omitempty"`
	JSON    bool   `json:"json,omitempty"` // request JSON output from server
}

// ServiceInfo is a single service record in list/status responses.
type ServiceInfo struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	PID      int    `json:"pid,omitempty"`
	Restarts int    `json:"restarts"`
	Since    string `json:"since"`
	Enabled  bool   `json:"enabled"`
}

// Response is the JSON structure returned by the manager.
type Response struct {
	OK       bool          `json:"ok"`
	Error    string        `json:"error,omitempty"`
	Services []ServiceInfo `json:"services,omitempty"`
	Service  *ServiceInfo  `json:"service,omitempty"`
	Message  string        `json:"message,omitempty"`
	Logs     []string      `json:"logs,omitempty"`
}
