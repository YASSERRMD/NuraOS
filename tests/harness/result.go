package harness

// Status is the outcome of a test case.
type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
)

// Evidence holds paths and data captured on failure.
type Evidence struct {
	SerialLogPath   string `json:"serial_log_path,omitempty"`
	JournalExcerpt  string `json:"journal_excerpt,omitempty"`
	MetricsSnapshot string `json:"metrics_snapshot,omitempty"`
	ConfigUsed      string `json:"config_used,omitempty"`
}

// Result is the outcome of one test case.
type Result struct {
	Suite    string   `json:"suite"`
	Case     string   `json:"case"`
	Status   Status   `json:"status"`
	Duration float64  `json:"duration_ms"`
	Message  string   `json:"message,omitempty"`
	Evidence Evidence `json:"evidence,omitempty"`
}

// SuiteRun groups results for one suite execution.
type SuiteRun struct {
	Suite   string   `json:"suite"`
	Results []Result `json:"results"`
}
