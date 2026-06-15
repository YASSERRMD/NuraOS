// Package journal implements the NuraOS system log.
//
// Records are stored as newline-delimited JSON under /data/journal/.
// Each file covers one day. Rotation deletes the oldest files when total
// usage exceeds the configured size cap. All writes are append-only.
//
// The journal captures:
//   - stdout and stderr from all service units (tagged by service name)
//   - kernel messages from /dev/kmsg (tagged as "kernel")
//
// Priority levels follow syslog conventions (RFC 5424).
package journal

import (
	"encoding/json"
	"time"
)

// Priority is a syslog-compatible log priority level.
type Priority int

const (
	PriEmergency Priority = 0
	PriAlert     Priority = 1
	PriCritical  Priority = 2
	PriError     Priority = 3
	PriWarning   Priority = 4
	PriNotice    Priority = 5
	PriInfo      Priority = 6
	PriDebug     Priority = 7
)

var priNames = [...]string{
	"emergency", "alert", "critical", "error",
	"warning", "notice", "info", "debug",
}

func (p Priority) String() string {
	if int(p) < len(priNames) {
		return priNames[p]
	}
	return "unknown"
}

// ParsePriority converts a string priority name to Priority.
// Returns PriInfo and ok=false if unknown.
func ParsePriority(s string) (Priority, bool) {
	for i, name := range priNames {
		if name == s {
			return Priority(i), true
		}
	}
	return PriInfo, false
}

// Record is one journal entry.
type Record struct {
	Time    time.Time `json:"ts"`
	Service string    `json:"svc"`
	PID     int       `json:"pid,omitempty"`
	Pri     Priority  `json:"pri"`
	Message string    `json:"msg"`
}

// MarshalNDJSON encodes r as a single JSON line with a newline terminator.
func (r Record) MarshalNDJSON() ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// UnmarshalNDJSON decodes one newline-delimited JSON journal line.
func UnmarshalNDJSON(line []byte) (Record, error) {
	var r Record
	if err := json.Unmarshal(line, &r); err != nil {
		return Record{}, err
	}
	return r, nil
}
