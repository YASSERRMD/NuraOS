package update

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogEntry is one line in the update audit log.
type LogEntry struct {
	Timestamp string `json:"ts"`
	TxID      string `json:"tx_id,omitempty"`
	Event     string `json:"event"`
	Detail    string `json:"detail,omitempty"`
}

// AuditLog appends structured log entries to the update audit log file.
// Each entry is a newline-terminated JSON object.
type AuditLog struct {
	path string
	mu   sync.Mutex
}

// NewAuditLog returns an AuditLog that writes to dataDir/update/audit.log.
func NewAuditLog(dataDir string) *AuditLog {
	return &AuditLog{path: filepath.Join(dataDir, "update", "audit.log")}
}

// Log appends an entry to the audit log. Errors are silently swallowed so that
// a logging failure never prevents an update from completing.
func (l *AuditLog) Log(txID, event, detail string) {
	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		TxID:      txID,
		Event:     event,
		Detail:    detail,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = os.MkdirAll(filepath.Dir(l.path), 0o755)
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s\n", data)
}

// Entries reads and returns all log entries. Returns an empty slice if the
// log file does not exist.
func (l *AuditLog) Entries() ([]LogEntry, error) {
	data, err := os.ReadFile(l.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []LogEntry
	for _, line := range splitLines(data) {
		var e LogEntry
		if json.Unmarshal([]byte(line), &e) == nil {
			entries = append(entries, e)
		}
	}
	return entries, nil
}

func splitLines(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, string(data[start:i]))
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
}
