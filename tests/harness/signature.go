package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// Volatile patterns stripped during normalisation so the same logical error
// produces the same failure signature regardless of run-specific values.
var (
	// ISO 8601 and similar timestamps.
	reISO8601 = regexp.MustCompile(
		`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?`)
	// Unix epoch seconds or milliseconds (10-13 digit bare numbers).
	reUnixTS = regexp.MustCompile(`\b\d{10,13}\b`)
	// PID patterns: pid=123, pid:123, [123], (pid 123).
	rePID = regexp.MustCompile(`(?i)(?:pid\s*[:=]\s*|pid\s+)\d+|\[\d{2,6}\]|\(pid\s+\d+\)`)
	// Hex memory addresses: 0x7f8a1b2c or 0xdeadbeef.
	reHexAddr = regexp.MustCompile(`\b0x[0-9a-fA-F]{4,}\b`)
	// IP:port pairs (IPv4 and IPv6 loopback).
	reIPPort = regexp.MustCompile(
		`\b\d{1,3}(?:\.\d{1,3}){3}:\d{2,5}\b|\[?::1\]?:\d{2,5}`)
	// Bare high-numbered ports such as localhost:53291.
	reHighPort = regexp.MustCompile(`:\d{4,5}\b`)
)

// Normalise strips volatile portions from text (timestamps, PIDs, memory
// addresses, port numbers) so that the same error produces the same string
// on every run. Used as the input to FailureSignature.
func Normalise(text string) string {
	s := reISO8601.ReplaceAllString(text, "<TS>")
	s = reUnixTS.ReplaceAllString(s, "<TS>")
	s = rePID.ReplaceAllString(s, "<PID>")
	s = reHexAddr.ReplaceAllString(s, "<ADDR>")
	s = reIPPort.ReplaceAllString(s, "<ADDR>:<PORT>")
	s = reHighPort.ReplaceAllString(s, ":<PORT>")
	return strings.TrimSpace(s)
}

// FailureSignature returns a stable 16-char lowercase hex string for a failing
// case. It is the first 8 bytes of SHA-256(suite:case:Normalise(message)).
// The same logical failure across different runs produces the same signature,
// enabling deduplication when filing GitHub issues.
func FailureSignature(suite, case_, message string) string {
	input := suite + ":" + case_ + ":" + Normalise(message)
	h := sha256.Sum256([]byte(input))
	return hex.EncodeToString(h[:8])
}
