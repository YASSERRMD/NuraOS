package harness

// Status is the outcome of a test case.
type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
)

// Evidence holds paths and content captured when a case fails.
// All text in this struct has been redacted before storage.
type Evidence struct {
	// BundleDir is the directory containing all captured files for this failure.
	BundleDir string `json:"bundle_dir,omitempty"`
	// SerialLogPath is the path to the redacted serial console log.
	SerialLogPath string `json:"serial_log_path,omitempty"`
	// JournalExcerpt is the last 100 lines of the serial log (inline).
	JournalExcerpt string `json:"journal_excerpt,omitempty"`
	// MetricsSnapshot is the path to the redacted /metrics scrape.
	MetricsSnapshot string `json:"metrics_snapshot,omitempty"`
	// ConfigDump is the path to the redacted /config response.
	ConfigDump string `json:"config_dump,omitempty"`
}

// Result is the outcome of one test case. It carries stable identifiers
// (run_id, commit_sha) so all results from one run are correlated, and a
// failure_signature for deduplication across runs.
type Result struct {
	// RunID is a random hex string shared by all results in the same run.
	RunID string `json:"run_id,omitempty"`
	// CommitSHA is the HEAD commit at the time of the run.
	CommitSHA string `json:"commit_sha,omitempty"`
	Suite     string `json:"suite"`
	Case      string `json:"case"`
	Status    Status `json:"status"`
	// Duration is the case wall-clock time in milliseconds.
	Duration float64 `json:"duration_ms"`
	// Message is a human-readable summary; secrets are redacted.
	Message string `json:"message,omitempty"`
	// FailureSignature is a stable 16-char hex hash of suite+case+normalised
	// error. Set only when Status is fail. Used for GitHub issue deduplication.
	FailureSignature string   `json:"failure_signature,omitempty"`
	Evidence         Evidence `json:"evidence,omitempty"`
}

// SuiteRun groups results for one suite execution.
type SuiteRun struct {
	Suite   string   `json:"suite"`
	Results []Result `json:"results"`
}
