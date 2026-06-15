package update

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	txFileName = "tx.json"
	stagingDir = "update/staging"
)

// ErrNoActiveSlot is returned when the active-slot file cannot be read.
var ErrNoActiveSlot = errors.New("cannot determine active slot")

// ErrBadImageSHA is returned when the staged image SHA-256 does not match.
var ErrBadImageSHA = errors.New("image SHA-256 mismatch")

// ErrBadImageSig is returned when the image signature verification fails.
var ErrBadImageSig = errors.New("image signature verification failed")

// ErrTxExists is returned when a transaction is already in progress.
var ErrTxExists = errors.New("update transaction already in progress; abort it first")

// Apply executes a full update transaction:
//  1. Stage the image from r into the inactive slot's staging area.
//  2. Verify SHA-256 (expectedSHA may be empty to skip).
//  3. Verify Ed25519 signature from sigBytes (may be nil to skip; not recommended).
//  4. Snapshot health state.
//  5. Commit atomically: rename staged file into /boot/rootfs-<slot>.ext4,
//     flip active-slot.
//
// On any failure the staged file and transaction record are cleaned up.
// The update audit log is written throughout.
func Apply(r io.Reader, source, expectedSHA string, sigBytes []byte, runningServices []string, opts Options) (*Transaction, error) {
	log := NewAuditLog(opts.dataDir())

	// Check for an in-progress transaction.
	txPath := filepath.Join(opts.dataDir(), txFileName)
	if _, err := os.Stat(txPath); err == nil {
		return nil, ErrTxExists
	}

	activeSlot, err := readActiveSlot(opts.dataDir())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoActiveSlot, err)
	}
	inactiveSlot := other(activeSlot)

	txID := shortID()
	stagingPath := filepath.Join(opts.dataDir(), stagingDir, txID, "rootfs-"+inactiveSlot+".ext4")
	if err := os.MkdirAll(filepath.Dir(stagingPath), 0o755); err != nil {
		return nil, fmt.Errorf("create staging dir: %w", err)
	}

	tx := &Transaction{
		ID:         txID,
		State:      TxStaging,
		TargetSlot: inactiveSlot,
		Source:     source,
		StagedPath: stagingPath,
		ExpectedSHA: expectedSHA,
		StartedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveTx(opts.dataDir(), tx); err != nil {
		return nil, fmt.Errorf("save tx record: %w", err)
	}
	log.Log(txID, "tx.begin", fmt.Sprintf("staging to inactive slot %s from %s", inactiveSlot, source))

	// --- Stage ---
	actualSHA, err := stageImage(r, stagingPath)
	if err != nil {
		abortTx(opts.dataDir(), tx, log, fmt.Sprintf("staging failed: %v", err))
		return nil, fmt.Errorf("stage image: %w", err)
	}
	log.Log(txID, "tx.staged", fmt.Sprintf("sha256=%s size_ok", actualSHA))

	// --- Verify SHA-256 ---
	tx.State = TxVerifying
	_ = saveTx(opts.dataDir(), tx)

	if expectedSHA != "" && !strings.EqualFold(actualSHA, expectedSHA) {
		abortTx(opts.dataDir(), tx, log, fmt.Sprintf("sha256 mismatch: got %s want %s", actualSHA, expectedSHA))
		return nil, fmt.Errorf("%w: got %s want %s", ErrBadImageSHA, actualSHA, expectedSHA)
	}

	// --- Verify signature (if key provided) ---
	if len(opts.PubKey) > 0 && sigBytes != nil {
		pub := ed25519.PublicKey(opts.PubKey)
		if !ed25519.Verify(pub, []byte(actualSHA), sigBytes) {
			abortTx(opts.dataDir(), tx, log, "signature verification failed")
			return nil, ErrBadImageSig
		}
		log.Log(txID, "tx.sig_ok", "ed25519 signature verified")
	}

	// --- Health snapshot ---
	tx.Health = &HealthSnapshot{
		ActiveSlot:      activeSlot,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		ServicesRunning: runningServices,
	}
	_ = saveTx(opts.dataDir(), tx)

	// --- Commit ---
	targetPath := filepath.Join(opts.rootfsDir(), "rootfs-"+inactiveSlot+".ext4")
	if err := atomicReplace(stagingPath, targetPath); err != nil {
		abortTx(opts.dataDir(), tx, log, fmt.Sprintf("atomic replace failed: %v", err))
		return nil, fmt.Errorf("commit to boot slot: %w", err)
	}

	if err := writeActiveSlot(opts.dataDir(), inactiveSlot); err != nil {
		// Critical: image written but slot not flipped. Log and leave tx as staging.
		log.Log(txID, "tx.commit_partial", fmt.Sprintf("slot flip failed: %v (manual recovery needed)", err))
		return nil, fmt.Errorf("flip active slot: %w", err)
	}

	tx.State = TxCommitted
	tx.CommittedAt = time.Now().UTC().Format(time.RFC3339)
	_ = saveTx(opts.dataDir(), tx)
	log.Log(txID, "tx.committed", fmt.Sprintf("active slot now %s", inactiveSlot))

	// Clean up staging directory.
	_ = os.RemoveAll(filepath.Dir(stagingPath))

	return tx, nil
}

// Abort cancels the current in-progress transaction, deleting staged files.
// Returns ErrTxExists-style nil if there is no transaction to abort.
func Abort(opts Options) error {
	log := NewAuditLog(opts.dataDir())
	tx, err := LoadTx(opts.dataDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	abortTx(opts.dataDir(), tx, log, "manual abort")
	return nil
}

// LoadTx reads the current transaction record.
func LoadTx(dataDir string) (*Transaction, error) {
	path := filepath.Join(dataDir, txFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tx Transaction
	if err := json.Unmarshal(data, &tx); err != nil {
		return nil, fmt.Errorf("parse tx record: %w", err)
	}
	return &tx, nil
}

// --- helpers ---

func stageImage(r io.Reader, dst string) (sha256Hex string, err error) {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	defer func() {
		cerr := f.Close()
		if err == nil {
			err = cerr
		}
	}()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func atomicReplace(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

func saveTx(dataDir string, tx *Transaction) error {
	data, err := json.MarshalIndent(tx, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dataDir, txFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func abortTx(dataDir string, tx *Transaction, log *AuditLog, reason string) {
	_ = os.RemoveAll(filepath.Dir(tx.StagedPath))
	tx.State = TxAborted
	tx.AbortedAt = time.Now().UTC().Format(time.RFC3339)
	tx.AbortReason = reason
	_ = saveTx(dataDir, tx)
	log.Log(tx.ID, "tx.aborted", reason)
}

func readActiveSlot(dataDir string) (string, error) {
	slotFile := filepath.Join(dataDir, "etc", "active-slot")
	data, err := os.ReadFile(slotFile)
	if errors.Is(err, os.ErrNotExist) {
		return "a", nil // default
	}
	if err != nil {
		return "", err
	}
	slot := strings.TrimSpace(string(data))
	if slot != "a" && slot != "b" {
		return "", fmt.Errorf("invalid slot %q", slot)
	}
	return slot, nil
}

// WriteActiveSlot atomically writes the active slot file.
func WriteActiveSlot(dataDir, slot string) error {
	return writeActiveSlot(dataDir, slot)
}

func writeActiveSlot(dataDir, slot string) error {
	slotFile := filepath.Join(dataDir, "etc", "active-slot")
	if err := os.MkdirAll(filepath.Dir(slotFile), 0o755); err != nil {
		return err
	}
	tmp := slotFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(slot+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, slotFile)
}

func other(slot string) string {
	if slot == "a" {
		return "b"
	}
	return "a"
}

// shortID generates a short unique transaction ID from the current timestamp.
func shortID() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}
