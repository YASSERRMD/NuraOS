package journal

import (
	"regexp"
	"strings"
)

// DefaultRedactPatterns matches common secret field names in log messages.
// The Forwarder uses these when no explicit patterns are configured.
var DefaultRedactPatterns = []*regexp.Regexp{
	// key=value assignments for common secret field names (case-insensitive).
	regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|apikey|auth[_-]?key|credential)=\S+`),
	// Bearer tokens in Authorization headers.
	regexp.MustCompile(`(?i)bearer\s+\S+`),
}

// Redact replaces the value portion of secret-looking substrings in msg with
// [REDACTED]. The key name (the part before = or whitespace) is preserved so
// that the log entry still indicates which field was present.
func Redact(msg string, patterns []*regexp.Regexp) string {
	for _, p := range patterns {
		msg = p.ReplaceAllStringFunc(msg, func(m string) string {
			for _, sep := range []string{"=", " ", "\t"} {
				if idx := strings.Index(m, sep); idx >= 0 {
					return m[:idx+1] + "[REDACTED]"
				}
			}
			return "[REDACTED]"
		})
	}
	return msg
}
