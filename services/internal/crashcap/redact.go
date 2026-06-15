// Package crashcap captures minimal crash diagnostics when a service exits
// unexpectedly, writes them to /data/crashes, redacts secrets, and rotates
// old captures so the directory stays bounded in size.
package crashcap

import (
	"regexp"
	"strings"
)

// redactPatterns matches common secret shapes that should be scrubbed from
// crash captures before they are written to disk or bundled into archives.
var redactPatterns = []*regexp.Regexp{
	// Bearer / API tokens: TOKEN=xxx, API_KEY=xxx, BEARER=xxx
	regexp.MustCompile(`(?i)(token|api[_-]?key|bearer|secret|password|passwd|auth)\s*[=:]\s*\S+`),
	// Authorization header value
	regexp.MustCompile(`(?i)Authorization:\s*\S+\s+\S+`),
	// Private-key PEM block
	regexp.MustCompile(`-----BEGIN [A-Z ]+PRIVATE KEY-----[\s\S]*?-----END [A-Z ]+PRIVATE KEY-----`),
	// AWS-style access keys: AKIA[A-Z0-9]{16}
	regexp.MustCompile(`AKIA[A-Z0-9]{16}`),
	// Generic 32+ hex strings that look like tokens
	regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`),
}

const redactedPlaceholder = "[REDACTED]"

// RedactLine scrubs recognised secret patterns from a single log line.
func RedactLine(line string) string {
	for _, pat := range redactPatterns {
		line = pat.ReplaceAllStringFunc(line, func(m string) string {
			// Preserve the key name for pattern 0 so the field is still
			// identifiable in the output.
			if idx := strings.IndexAny(m, "=:"); idx != -1 {
				key := m[:idx+1]
				return key + redactedPlaceholder
			}
			return redactedPlaceholder
		})
	}
	return line
}

// RedactBytes applies secret redaction to an entire byte slice line-by-line
// and returns the sanitised result.
func RedactBytes(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		lines[i] = RedactLine(line)
	}
	return []byte(strings.Join(lines, "\n"))
}
