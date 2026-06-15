package backup_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/backup"
)

// populateDataDir creates a small synthetic /data tree for testing.
func populateDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"config/nura.json":      `{"version":1}`,
		"sessions/s1.json":      `{"id":"s1","turns":[]}`,
		"models/big.gguf":       strings.Repeat("x", 1024), // should be excluded
		"crashes/gateway.json":  `{"service":"gateway"}`,
	}
	for rel, content := range files {
		abs := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(abs), 0755)
		_ = os.WriteFile(abs, []byte(content), 0644)
	}
	return dir
}

// TestBackupRunCreatesArchive verifies a .tar.gz archive is created.
func TestBackupRunCreatesArchive(t *testing.T) {
	src := populateDataDir(t)
	out := filepath.Join(t.TempDir(), "backup.tar.gz")

	m, err := backup.Run(backup.Options{
		DataDir:       src,
		OutPath:       out,
		ExcludeModels: true,
	})
	if err != nil {
		t.Fatalf("backup.Run: %v", err)
	}
	if m.FileCount == 0 {
		t.Error("backup: FileCount = 0; expected > 0")
	}
	if m.SHA256 == "" {
		t.Error("backup: SHA256 empty")
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("archive not created: %v", err)
	}
}

// TestBackupExcludesModels verifies model blobs are not in the archive.
func TestBackupExcludesModels(t *testing.T) {
	src := populateDataDir(t)
	out := filepath.Join(t.TempDir(), "backup.tar.gz")

	m, err := backup.Run(backup.Options{
		DataDir:       src,
		OutPath:       out,
		ExcludeModels: true,
	})
	if err != nil {
		t.Fatalf("backup.Run: %v", err)
	}
	if !m.ExcludedModels {
		t.Error("ExcludedModels should be true")
	}

	// Restore to a clean dir and verify models/ is absent.
	dst := t.TempDir()
	res, err := backup.Restore(backup.RestoreOptions{
		ArchivePath:    out,
		DestDir:        dst,
		ExpectedSHA256: m.SHA256,
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if res.FilesRestored == 0 {
		t.Error("Restore: FilesRestored = 0")
	}
	// models/big.gguf must not have been restored.
	if _, err := os.Stat(filepath.Join(dst, "models", "big.gguf")); err == nil {
		t.Error("models/big.gguf was restored despite ExcludeModels=true")
	}
}

// TestRestoreSHA256Mismatch verifies tampered archives are rejected.
func TestRestoreSHA256Mismatch(t *testing.T) {
	src := populateDataDir(t)
	out := filepath.Join(t.TempDir(), "backup.tar.gz")
	m, _ := backup.Run(backup.Options{DataDir: src, OutPath: out, ExcludeModels: true})

	_, err := backup.Restore(backup.RestoreOptions{
		ArchivePath:    out,
		DestDir:        t.TempDir(),
		ExpectedSHA256: m.SHA256[:len(m.SHA256)-2] + "ff", // tampered
	})
	if err == nil {
		t.Error("expected SHA-256 mismatch error; got nil")
	}
}

// TestRestoreDryRun verifies dry-run lists files without writing.
func TestRestoreDryRun(t *testing.T) {
	src := populateDataDir(t)
	out := filepath.Join(t.TempDir(), "backup.tar.gz")
	backup.Run(backup.Options{DataDir: src, OutPath: out, ExcludeModels: true}) //nolint

	dst := t.TempDir()
	res, err := backup.Restore(backup.RestoreOptions{
		ArchivePath: out,
		DestDir:     dst,
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("dry-run Restore: %v", err)
	}
	if len(res.Preview) == 0 {
		t.Error("dry-run: Preview is empty")
	}
	if res.FilesRestored != 0 {
		t.Errorf("dry-run: FilesRestored = %d; want 0", res.FilesRestored)
	}
}

// TestEncryptDecryptRoundtrip verifies encrypted backup restores correctly.
func TestEncryptDecryptRoundtrip(t *testing.T) {
	src := populateDataDir(t)
	out := filepath.Join(t.TempDir(), "enc-backup.tar.gz")
	passphrase := "test-secret-passphrase"

	m, err := backup.Run(backup.Options{
		DataDir:       src,
		OutPath:       out,
		ExcludeModels: true,
		Passphrase:    passphrase,
	})
	if err != nil {
		t.Fatalf("backup.Run encrypted: %v", err)
	}
	if !m.Encrypted {
		t.Error("Manifest.Encrypted should be true")
	}

	dst := t.TempDir()
	res, err := backup.Restore(backup.RestoreOptions{
		ArchivePath:    out,
		DestDir:        dst,
		Passphrase:     passphrase,
		ExpectedSHA256: m.SHA256,
	})
	if err != nil {
		t.Fatalf("Restore encrypted: %v", err)
	}
	if res.FilesRestored == 0 {
		t.Error("encrypted restore: FilesRestored = 0")
	}
}

// TestWrongPassphraseFailsDecrypt verifies wrong passphrase is rejected.
func TestWrongPassphraseFailsDecrypt(t *testing.T) {
	src := populateDataDir(t)
	out := filepath.Join(t.TempDir(), "enc.tar.gz")
	backup.Run(backup.Options{DataDir: src, OutPath: out, Passphrase: "right"}) //nolint

	_, err := backup.Restore(backup.RestoreOptions{
		ArchivePath: out,
		DestDir:     t.TempDir(),
		Passphrase:  "wrong",
	})
	if err == nil {
		t.Error("expected decryption error with wrong passphrase; got nil")
	}
}

// TestManifestRoundtrip verifies WriteManifest / ReadManifest work correctly.
func TestManifestRoundtrip(t *testing.T) {
	src := populateDataDir(t)
	out := filepath.Join(t.TempDir(), "backup.tar.gz")
	m, err := backup.Run(backup.Options{DataDir: src, OutPath: out, ExcludeModels: true})
	if err != nil {
		t.Fatalf("backup.Run: %v", err)
	}
	if err := backup.WriteManifest(out, m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	m2, err := backup.ReadManifest(out)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if m2.SHA256 != m.SHA256 {
		t.Errorf("manifest SHA256 mismatch: got %s want %s", m2.SHA256, m.SHA256)
	}
	if m2.FileCount != m.FileCount {
		t.Errorf("manifest FileCount mismatch: got %d want %d", m2.FileCount, m.FileCount)
	}
}
