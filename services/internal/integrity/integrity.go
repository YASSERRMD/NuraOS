// Package integrity implements boot-time integrity verification for NuraOS.
//
// At boot the initramfs shell script checks /data/etc/boot-hashes (sha256sum
// format) and writes the result to /run/nura-integrity-status. The Go runtime
// (nura-manager / gateway) reads that status and can perform a deeper Ed25519
// signature check via VerifyManifest.
//
// Fail-closed: any hash mismatch drops the init script into a recovery shell
// before the supervisor starts.
package integrity

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	// DefaultManifestPath is the default path for the signed boot manifest.
	DefaultManifestPath = "/data/etc/boot-manifest.json"
	// DefaultSigPath is the default path for the manifest Ed25519 signature.
	DefaultSigPath = "/data/etc/boot-manifest.sig"
	// DefaultHashesPath is the sha256sum-compatible file used by the shell init.
	DefaultHashesPath = "/data/etc/boot-hashes"
	// DefaultStatusPath is where the boot-time verification result is written.
	DefaultStatusPath = "/run/nura-integrity-status"
)

// Entry is one file entry in the boot manifest.
type Entry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Manifest is the boot integrity manifest.
type Manifest struct {
	Schema    int     `json:"schema"`
	Timestamp string  `json:"timestamp"`
	Slot      string  `json:"slot,omitempty"`
	Entries   []Entry `json:"entries"`
}

// Status records the result of integrity verification written by init.
type Status struct {
	Result    string `json:"result"`           // "pass" | "fail" | "unknown" | "skipped"
	Timestamp string `json:"timestamp"`
	Detail    string `json:"detail,omitempty"` // human-readable context on failure
}

var (
	// ErrBadSignature is returned when Ed25519 verification fails.
	ErrBadSignature = errors.New("integrity: signature verification failed")
	// ErrHashMismatch is returned when a file's SHA-256 does not match the manifest.
	ErrHashMismatch = errors.New("integrity: SHA-256 mismatch")
	// ErrManifestEmpty is returned when the manifest contains no entries.
	ErrManifestEmpty = errors.New("integrity: manifest has no entries")
)

// VerifyFile checks the SHA-256 of a file against an expected hex digest.
func VerifyFile(path, expectedSHA256 string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash %s: %w", path, err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != expectedSHA256 {
		return fmt.Errorf("%w: %s: got %s want %s", ErrHashMismatch, path, got, expectedSHA256)
	}
	return nil
}

// VerifyManifest loads the JSON manifest from manifestPath, verifies the
// Ed25519 signature from sigPath (base64-encoded raw signature bytes), then
// verifies each entry's SHA-256. Returns the parsed manifest on success.
func VerifyManifest(manifestPath, sigPath string, pubKey ed25519.PublicKey) (*Manifest, error) {
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	// The canonical signed form is the JSON without trailing whitespace.
	canonicalData := bytes.TrimRight(manifestData, "\n\r ")

	sigRaw, err := os.ReadFile(sigPath)
	if err != nil {
		return nil, fmt.Errorf("read signature: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(sigRaw)))
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}

	if !ed25519.Verify(pubKey, canonicalData, sig) {
		return nil, ErrBadSignature
	}

	var m Manifest
	if err := json.Unmarshal(canonicalData, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if len(m.Entries) == 0 {
		return nil, ErrManifestEmpty
	}

	for _, e := range m.Entries {
		if err := VerifyFile(e.Path, e.SHA256); err != nil {
			return nil, err
		}
	}
	return &m, nil
}

// GenerateManifest hashes each path in files, writes the manifest JSON to
// manifestPath, signs it with privKey, and writes the base64 signature to
// sigPath. Also writes a sha256sum-compatible hashes file alongside the manifest.
func GenerateManifest(manifestPath, sigPath string, slot string, files []string, privKey ed25519.PrivateKey) (*Manifest, error) {
	m := Manifest{
		Schema:    1,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Slot:      slot,
	}
	for _, path := range files {
		sum, err := hashFile(path)
		if err != nil {
			return nil, err
		}
		m.Entries = append(m.Entries, Entry{Path: path, SHA256: sum})
	}

	manifestData, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(manifestPath, append(manifestData, '\n'), 0o644); err != nil {
		return nil, err
	}

	sig := ed25519.Sign(privKey, manifestData)
	sigB64 := base64.StdEncoding.EncodeToString(sig) + "\n"
	if err := os.WriteFile(sigPath, []byte(sigB64), 0o644); err != nil {
		return nil, err
	}

	// Write sha256sum-compatible hashes file next to the manifest.
	hashesPath := filepath.Join(filepath.Dir(manifestPath), "boot-hashes")
	hf, err := os.Create(hashesPath)
	if err != nil {
		return nil, fmt.Errorf("create hashes file: %w", err)
	}
	defer hf.Close()
	for _, e := range m.Entries {
		fmt.Fprintf(hf, "%s  %s\n", e.SHA256, e.Path)
	}

	return &m, nil
}

// WriteStatus persists a Status to path as JSON (atomic write).
func WriteStatus(path string, s Status) error {
	if s.Timestamp == "" {
		s.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadStatus reads an integrity status written by WriteStatus or the init
// script. Returns (Status{}, false) if the file does not exist or is malformed.
func ReadStatus(path string) (Status, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Status{}, false
	}
	var s Status
	if err := json.Unmarshal(bytes.TrimSpace(data), &s); err != nil {
		return Status{}, false
	}
	return s, true
}

// hashFile returns the hex-encoded SHA-256 of the file at path.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
