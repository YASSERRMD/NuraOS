package update_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/update"
)

func prepareEnv(t *testing.T) (dataDir, rootfsDir string, opts update.Options) {
	t.Helper()
	dir := t.TempDir()
	dataDir = dir
	rootfsDir = filepath.Join(dir, "boot")
	if err := os.MkdirAll(rootfsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Seed active slot = a
	etcDir := filepath.Join(dataDir, "etc")
	os.MkdirAll(etcDir, 0o755)
	os.WriteFile(filepath.Join(etcDir, "active-slot"), []byte("a\n"), 0o644)
	// Seed existing rootfs images (tiny placeholder files)
	os.WriteFile(filepath.Join(rootfsDir, "rootfs-a.ext4"), []byte("slot-a"), 0o644)
	os.WriteFile(filepath.Join(rootfsDir, "rootfs-b.ext4"), []byte("slot-b"), 0o644)
	opts = update.Options{DataDir: dataDir, RootfsDir: rootfsDir}
	return
}

func imageAndSHA(content string) ([]byte, string) {
	data := []byte(content)
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:])
}

func TestApplyBasic(t *testing.T) {
	dataDir, _, opts := prepareEnv(t)

	imgData, imgSHA := imageAndSHA("fake rootfs image content")
	tx, err := update.Apply(bytes.NewReader(imgData), "local", imgSHA, nil, nil, opts)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if tx.State != update.TxCommitted {
		t.Errorf("state = %s; want committed", tx.State)
	}
	if tx.TargetSlot != "b" {
		t.Errorf("target_slot = %s; want b (inactive when active=a)", tx.TargetSlot)
	}

	// Active slot should now be b.
	data, _ := os.ReadFile(filepath.Join(dataDir, "etc", "active-slot"))
	if strings.TrimSpace(string(data)) != "b" {
		t.Errorf("active-slot = %q; want b", strings.TrimSpace(string(data)))
	}
}

func TestApplyBadSHA(t *testing.T) {
	_, _, opts := prepareEnv(t)

	imgData, _ := imageAndSHA("content")
	_, err := update.Apply(bytes.NewReader(imgData), "local", "badhash", nil, nil, opts)
	if err == nil {
		t.Fatal("expected SHA mismatch error, got nil")
	}
}

func TestApplyBadSignature(t *testing.T) {
	_, _, opts := prepareEnv(t)

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	imgData, imgSHA := imageAndSHA("image")

	// Sign with one key but verify with a different (unrelated) key.
	sig := ed25519.Sign(priv, []byte(imgSHA))
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)

	opts.PubKey = otherPub
	_, err := update.Apply(bytes.NewReader(imgData), "local", imgSHA, sig, nil, opts)
	if err == nil {
		t.Fatal("expected signature error, got nil")
	}
}

func TestApplyWithValidSignature(t *testing.T) {
	_, _, opts := prepareEnv(t)

	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	imgData, imgSHA := imageAndSHA("signed image content")
	sig := ed25519.Sign(priv, []byte(imgSHA))

	opts.PubKey = pub
	tx, err := update.Apply(bytes.NewReader(imgData), "local", imgSHA, sig, []string{"gateway", "nura-agent"}, opts)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if tx.State != update.TxCommitted {
		t.Errorf("state = %s; want committed", tx.State)
	}
	if tx.Health == nil || len(tx.Health.ServicesRunning) != 2 {
		t.Error("health snapshot missing or wrong service count")
	}
}

func TestRecoveryInterrupted(t *testing.T) {
	dataDir, _, opts := prepareEnv(t)

	// Simulate an interrupted transaction in "staging" state.
	tx := &update.Transaction{
		ID:         "test-tx",
		State:      update.TxStaging,
		TargetSlot: "b",
		StagedPath: filepath.Join(dataDir, "update", "staging", "test-tx", "rootfs-b.ext4"),
		StartedAt:  "2026-01-01T00:00:00Z",
	}
	// Use exported SaveTx via a helper, or write the JSON manually.
	txJSON := `{"id":"test-tx","state":"staging","target_slot":"b","staged_path":"` +
		tx.StagedPath + `","sha256":"","started_at":"2026-01-01T00:00:00Z"}`
	os.WriteFile(filepath.Join(dataDir, "tx.json"), []byte(txJSON), 0o644)

	result, err := update.CheckRecovery(opts)
	if err != nil {
		t.Fatalf("CheckRecovery: %v", err)
	}
	if result == nil || !result.Found {
		t.Fatal("expected recovery to find an interrupted transaction")
	}
	if result.Action != "aborted" {
		t.Errorf("action = %q; want aborted", result.Action)
	}
}

func TestRecoveryNoop(t *testing.T) {
	_, _, opts := prepareEnv(t)

	result, err := update.CheckRecovery(opts)
	if err != nil {
		t.Fatalf("CheckRecovery: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result when no tx file, got %+v", result)
	}
}

func TestRollback(t *testing.T) {
	dataDir, _, opts := prepareEnv(t)

	imgData, imgSHA := imageAndSHA("rollback-test")
	tx, err := update.Apply(bytes.NewReader(imgData), "local", imgSHA, nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if tx.State != update.TxCommitted {
		t.Fatal("expected committed state before rollback")
	}

	prev, err := update.RollbackLastUpdate(opts)
	if err != nil {
		t.Fatalf("RollbackLastUpdate: %v", err)
	}
	if prev != "a" {
		t.Errorf("rolled back to %q; want a", prev)
	}

	data, _ := os.ReadFile(filepath.Join(dataDir, "etc", "active-slot"))
	if strings.TrimSpace(string(data)) != "a" {
		t.Errorf("active-slot after rollback = %q; want a", strings.TrimSpace(string(data)))
	}
}

func TestAuditLog(t *testing.T) {
	dir := t.TempDir()
	log := update.NewAuditLog(dir)
	log.Log("tx1", "tx.begin", "staging from local")
	log.Log("tx1", "tx.committed", "slot b")

	entries, err := log.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}
	if entries[0].Event != "tx.begin" {
		t.Errorf("entries[0].Event = %q; want tx.begin", entries[0].Event)
	}
}
