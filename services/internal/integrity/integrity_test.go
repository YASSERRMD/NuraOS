package integrity_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/integrity"
)

func hexSHA(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func writeTempFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVerifyFileOK(t *testing.T) {
	dir := t.TempDir()
	data := []byte("nuraos integrity test")
	path := writeTempFile(t, dir, "test.bin", data)
	if err := integrity.VerifyFile(path, hexSHA(data)); err != nil {
		t.Fatalf("expected no error; got %v", err)
	}
}

func TestVerifyFileMismatch(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "test.bin", []byte("original"))
	wrongSHA := hexSHA([]byte("tampered"))
	err := integrity.VerifyFile(path, wrongSHA)
	if err == nil {
		t.Fatal("expected mismatch error; got nil")
	}
}

func TestVerifyFileNotFound(t *testing.T) {
	err := integrity.VerifyFile("/nonexistent/path/file.bin", hexSHA([]byte("x")))
	if err == nil {
		t.Fatal("expected error for missing file; got nil")
	}
}

func TestGenerateAndVerifyManifest(t *testing.T) {
	dir := t.TempDir()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	fileA := writeTempFile(t, dir, "kernel", []byte("fake kernel binary"))
	fileB := writeTempFile(t, dir, "initramfs", []byte("fake initramfs data"))

	manifestPath := filepath.Join(dir, "boot-manifest.json")
	sigPath := filepath.Join(dir, "boot-manifest.sig")

	_, err := integrity.GenerateManifest(manifestPath, sigPath, "a", []string{fileA, fileB}, priv)
	if err != nil {
		t.Fatalf("GenerateManifest: %v", err)
	}

	m, err := integrity.VerifyManifest(manifestPath, sigPath, pub)
	if err != nil {
		t.Fatalf("VerifyManifest: %v", err)
	}
	if m.Slot != "a" {
		t.Errorf("slot = %q; want a", m.Slot)
	}
	if len(m.Entries) != 2 {
		t.Errorf("entries = %d; want 2", len(m.Entries))
	}
}

func TestVerifyManifestBadSignature(t *testing.T) {
	dir := t.TempDir()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)

	file := writeTempFile(t, dir, "kernel", []byte("kernel"))
	manifestPath := filepath.Join(dir, "boot-manifest.json")
	sigPath := filepath.Join(dir, "boot-manifest.sig")

	if _, err := integrity.GenerateManifest(manifestPath, sigPath, "b", []string{file}, priv); err != nil {
		t.Fatal(err)
	}

	_, err := integrity.VerifyManifest(manifestPath, sigPath, otherPub)
	if err == nil {
		t.Fatal("expected signature error; got nil")
	}
}

func TestVerifyManifestTamperedFile(t *testing.T) {
	dir := t.TempDir()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	file := writeTempFile(t, dir, "kernel", []byte("original content"))
	manifestPath := filepath.Join(dir, "boot-manifest.json")
	sigPath := filepath.Join(dir, "boot-manifest.sig")

	if _, err := integrity.GenerateManifest(manifestPath, sigPath, "a", []string{file}, priv); err != nil {
		t.Fatal(err)
	}

	// Tamper with the file after manifest is generated.
	if err := os.WriteFile(file, []byte("tampered content"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := integrity.VerifyManifest(manifestPath, sigPath, pub)
	if err == nil {
		t.Fatal("expected hash mismatch; got nil")
	}
}

func TestHashesFileGenerated(t *testing.T) {
	dir := t.TempDir()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)

	file := writeTempFile(t, dir, "rootfs.ext4", []byte("rootfs image"))
	manifestPath := filepath.Join(dir, "boot-manifest.json")
	sigPath := filepath.Join(dir, "boot-manifest.sig")

	if _, err := integrity.GenerateManifest(manifestPath, sigPath, "a", []string{file}, priv); err != nil {
		t.Fatal(err)
	}

	hashesPath := filepath.Join(dir, "boot-hashes")
	data, err := os.ReadFile(hashesPath)
	if err != nil {
		t.Fatalf("boot-hashes not generated: %v", err)
	}
	if len(data) == 0 {
		t.Error("boot-hashes is empty")
	}
}

func TestWriteReadStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "integrity-status")

	s := integrity.Status{Result: "pass", Detail: "all 3 entries verified"}
	if err := integrity.WriteStatus(path, s); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}

	got, ok := integrity.ReadStatus(path)
	if !ok {
		t.Fatal("ReadStatus returned not-ok")
	}
	if got.Result != "pass" {
		t.Errorf("result = %q; want pass", got.Result)
	}
	if got.Timestamp == "" {
		t.Error("timestamp should be auto-set")
	}
}

func TestReadStatusMissing(t *testing.T) {
	_, ok := integrity.ReadStatus("/nonexistent/path/status")
	if ok {
		t.Error("expected not-ok for missing file; got ok")
	}
}
