package harness

import (
	"regexp"
	"strings"
)

// secretPatterns lists regexps that match secrets commonly found in NuraOS
// config files, log output, and HTTP responses.
var secretPatterns = []*regexp.Regexp{
	// Anthropic API key format: sk-ant-api03-...
	regexp.MustCompile(`sk-ant-[a-zA-Z0-9_-]{20,}`),
	// OpenAI / generic sk- key format.
	regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),
	// Bearer token in HTTP header or log line.
	regexp.MustCompile(`(?i)Bearer\s+[a-zA-Z0-9._~+/=-]{8,}`),
	// Key/token/password/secret as a value in config-style key=value or key: value text.
	regexp.MustCompile(`(?i)(api[_-]?key|api[_-]?token|password|secret|bearer_token)\s*[:=]\s*\S+`),
	// TOML/YAML quoted string values for sensitive keys.
	regexp.MustCompile(`(?i)(api[_-]?key|api[_-]?token|password|secret)\s*=\s*"[^"]+"`),
}

const redactedMark = "[REDACTED]"

// Redact replaces all known secret patterns in text with [REDACTED].
// It must be called on all evidence content before writing to disk or
// embedding in result messages.
func Redact(text string) string {
	s := text
	for _, re := range secretPatterns {
		s = re.ReplaceAllStringFunc(s, func(match string) string {
			// For key=value and key: value forms, preserve the key name.
			for _, sep := range []string{"=", ":"} {
				if idx := strings.Index(match, sep); idx >= 0 {
					return match[:idx+1] + redactedMark
				}
			}
			return redactedMark
		})
	}
	return s
}
