// Package locale enforces UTF-8 text handling across NuraOS subsystems.
//
// By default NuraOS runs with LANG=C.UTF-8 so that all text -- console
// output, log lines, HTTP request/response bodies, model input/output, and
// provenance metadata -- is valid UTF-8. No locale-specific collation or
// character mapping is applied; all text is treated as a sequence of Unicode
// code points.
//
// Operators may override the locale by creating /data/etc/locale with a
// single line containing a POSIX locale name (e.g. "en_US.UTF-8"). The
// package does NOT call setlocale(3) (which does not exist in Go), but it
// exports helpers that sanitise, validate, and round-trip multi-byte and
// RTL text safely.
package locale

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	// DefaultLocale is the system default locale.
	DefaultLocale = "C.UTF-8"
	// LocaleConfigPath is the operator-configurable locale file.
	LocaleConfigPath = "/data/etc/locale"
)

// Config holds the resolved locale settings.
type Config struct {
	// Locale is the POSIX locale name (always ending in .UTF-8).
	Locale string `json:"locale"`
	// Encoding is always "UTF-8" for NuraOS.
	Encoding string `json:"encoding"`
}

// Load reads the locale configuration from LocaleConfigPath. If the file
// does not exist or cannot be read, DefaultLocale is used. An invalid locale
// (not ending in .UTF-8) is rejected and DefaultLocale is returned instead.
func Load(path string) Config {
	if path == "" {
		path = LocaleConfigPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultConfig()
	}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !isUTF8Locale(line) {
			// Reject non-UTF-8 locales; fall back to default.
			return defaultConfig()
		}
		return Config{Locale: line, Encoding: "UTF-8"}
	}
	return defaultConfig()
}

// EnvVars returns the environment variable slice that should be prepended to
// every subprocess to enforce the locale. Pass these to os/exec.Cmd.Env.
func EnvVars(cfg Config) []string {
	return []string{
		"LANG=" + cfg.Locale,
		"LC_ALL=" + cfg.Locale,
		"PYTHONIOENCODING=utf-8",
		"PYTHONUTF8=1",
	}
}

// ValidateUTF8 reports whether data is a valid UTF-8 byte sequence.
func ValidateUTF8(data []byte) bool {
	return utf8.Valid(data)
}

// SanitiseUTF8 returns a copy of s with every invalid UTF-8 sequence replaced
// by the Unicode replacement character U+FFFD. All valid code points
// (including multi-byte sequences and RTL characters) are preserved intact.
func SanitiseUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if r == utf8.RuneError {
			if _, size := utf8.DecodeRuneInString(s[i:]); size == 1 {
				b.WriteRune(utf8.RuneError) // replacement character
				continue
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// RoundTrip encodes s to UTF-8 bytes and decodes back to a string, returning
// the decoded string and whether the round-trip was lossless.
func RoundTrip(s string) (string, bool) {
	b := []byte(s)
	decoded := string(b)
	return decoded, decoded == s && utf8.ValidString(decoded)
}

// IsRTL reports whether s contains any right-to-left code points (Arabic,
// Hebrew, or general strong-RTL categories). This is a heuristic based on
// Unicode block ranges; it does not implement full BiDi algorithm.
func IsRTL(s string) bool {
	for _, r := range s {
		if isRTLRune(r) {
			return true
		}
	}
	return false
}

// ValidateLocale reports whether locale is an acceptable UTF-8 locale name.
// NuraOS only accepts locales ending in ".UTF-8" (case-insensitive).
func ValidateLocale(locale string) error {
	if !isUTF8Locale(locale) {
		return fmt.Errorf("locale %q is not a UTF-8 locale; NuraOS requires a .UTF-8 suffix", locale)
	}
	return nil
}

// --- helpers ---

func defaultConfig() Config {
	return Config{Locale: DefaultLocale, Encoding: "UTF-8"}
}

func isUTF8Locale(s string) bool {
	return strings.HasSuffix(strings.ToUpper(s), ".UTF-8")
}

// isRTLRune returns true for code points in known RTL Unicode blocks.
func isRTLRune(r rune) bool {
	// Arabic: U+0600-U+06FF
	if r >= 0x0600 && r <= 0x06FF {
		return true
	}
	// Arabic Supplement: U+0750-U+077F
	if r >= 0x0750 && r <= 0x077F {
		return true
	}
	// Arabic Extended-A: U+08A0-U+08FF
	if r >= 0x08A0 && r <= 0x08FF {
		return true
	}
	// Hebrew: U+0590-U+05FF
	if r >= 0x0590 && r <= 0x05FF {
		return true
	}
	// Thaana (Maldivian): U+0780-U+07BF
	if r >= 0x0780 && r <= 0x07BF {
		return true
	}
	// N'Ko: U+07C0-U+07FF
	if r >= 0x07C0 && r <= 0x07FF {
		return true
	}
	return false
}
