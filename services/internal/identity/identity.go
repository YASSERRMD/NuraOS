// Package identity provides stable system identity: machine-id and hostname.
//
// The machine-id is a randomly generated UUID v4 (32-char lowercase hex,
// no dashes) persisted in /data/machine-id on first boot. It does not derive
// from any secret and is not considered sensitive.
package identity

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// OSVersion is the NuraOS release embedded at build time.
	OSVersion = "1.0.0"

	machineIDFile = "machine-id"
	hostnameFile  = "etc/hostname"
)

// LoadOrCreate reads the machine-id from dataDir/machine-id. When the file is
// absent or malformed it generates a new ID and persists it atomically.
// The ID is a 32-character lowercase hex UUID v4.
func LoadOrCreate(dataDir string) (string, error) {
	path := filepath.Join(dataDir, machineIDFile)
	if data, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(data))
		if len(id) == 32 {
			return id, nil
		}
	}

	id, err := generateID()
	if err != nil {
		return "", fmt.Errorf("generate machine-id: %w", err)
	}
	if err := atomicWrite(path, []byte(id+"\n")); err != nil {
		return "", fmt.Errorf("persist machine-id: %w", err)
	}
	return id, nil
}

// LoadHostname reads the configured hostname from dataDir/etc/hostname.
// Falls back to "nura-" + first 8 chars of machineID when the file is absent.
func LoadHostname(dataDir, machineID string) (string, error) {
	path := filepath.Join(dataDir, hostnameFile)
	if data, err := os.ReadFile(path); err == nil {
		h := strings.TrimSpace(string(data))
		if h != "" {
			return h, nil
		}
	}
	suffix := machineID
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	return "nura-" + suffix, nil
}

// generateID returns a new random 32-character lowercase hex UUID v4.
// The ID is cryptographically random and does not embed any system secret.
func generateID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x", b), nil
}

// atomicWrite writes data to path using write-to-temp-then-rename so that
// a crash mid-write does not leave a partial file.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
