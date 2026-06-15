package identity_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/identity"
)

// TestLoadOrCreateGenerates verifies first-boot ID generation.
func TestLoadOrCreateGenerates(t *testing.T) {
	dir := t.TempDir()
	id, err := identity.LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if len(id) != 32 {
		t.Errorf("expected 32-char id, got %d chars: %q", len(id), id)
	}
	if strings.ToLower(id) != id {
		t.Errorf("expected lowercase, got %q", id)
	}
}

// TestLoadOrCreateStable verifies the same ID is returned on repeated calls.
func TestLoadOrCreateStable(t *testing.T) {
	dir := t.TempDir()
	id1, err := identity.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := identity.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Errorf("ID changed between calls: %q vs %q", id1, id2)
	}
}

// TestLoadOrCreatePersisted verifies the ID is written to disk.
func TestLoadOrCreatePersisted(t *testing.T) {
	dir := t.TempDir()
	id, _ := identity.LoadOrCreate(dir)

	raw, err := os.ReadFile(filepath.Join(dir, "machine-id"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != id {
		t.Errorf("persisted %q, want %q", strings.TrimSpace(string(raw)), id)
	}
}

// TestLoadHostnameFallback verifies the auto-generated hostname when no file exists.
func TestLoadHostnameFallback(t *testing.T) {
	dir := t.TempDir()
	machineID := "abcdef1234567890abcdef1234567890"
	h, err := identity.LoadHostname(dir, machineID)
	if err != nil {
		t.Fatal(err)
	}
	if h != "nura-abcdef12" {
		t.Errorf("expected nura-abcdef12, got %q", h)
	}
}

// TestLoadHostnameFromFile verifies that a configured hostname is used.
func TestLoadHostnameFromFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "etc", "hostname"), []byte("mynode\n"), 0644); err != nil {
		t.Fatal(err)
	}
	h, err := identity.LoadHostname(dir, "00000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if h != "mynode" {
		t.Errorf("expected mynode, got %q", h)
	}
}

// TestGatherSysInfo verifies SysInfo fields are populated.
func TestGatherSysInfo(t *testing.T) {
	info := identity.Gather("aabbccddeeff00112233445566778899", "testhost", time.Now())
	if info.MachineID != "aabbccddeeff00112233445566778899" {
		t.Errorf("MachineID mismatch")
	}
	if info.Hostname != "testhost" {
		t.Errorf("Hostname mismatch")
	}
	if info.OSVersion == "" {
		t.Error("OSVersion empty")
	}
	if info.UptimeSec < 0 {
		t.Error("negative uptime")
	}
	if info.FormatStatus() == "" {
		t.Error("FormatStatus returned empty string")
	}
}

// TestSetHostnameNoError verifies SetHostname does not panic even when it
// fails (no CAP_SYS_ADMIN in test environments).
func TestSetHostnameNoError(t *testing.T) {
	// We don't assert success since it requires root; just assert no panic.
	_ = identity.SetHostname("test-node")
}
