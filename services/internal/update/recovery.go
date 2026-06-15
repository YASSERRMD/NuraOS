package update

import (
	"errors"
	"fmt"
	"os"
)

// RecoveryResult describes what CheckRecovery found and did.
type RecoveryResult struct {
	// Found is true when an interrupted transaction was detected.
	Found bool
	// TxID is the interrupted transaction ID.
	TxID string
	// State is the state the interrupted transaction was in.
	State TxState
	// Action describes what was done (e.g. "aborted").
	Action string
}

// CheckRecovery checks for an interrupted update transaction and cleans it up.
// It is safe to call on every boot; it is a no-op when no transaction exists or
// when the transaction is already in a terminal state (committed or aborted).
//
// An interrupted transaction (state staging or verifying) is aborted: its staged
// files are deleted and the transaction state is set to aborted.
//
// The function writes recovery events to the audit log.
func CheckRecovery(opts Options) (*RecoveryResult, error) {
	tx, err := LoadTx(opts.dataDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil // nothing to recover
	}
	if err != nil {
		return nil, fmt.Errorf("read tx record during recovery: %w", err)
	}

	switch tx.State {
	case TxCommitted, TxAborted:
		// Already in a terminal state; nothing to do.
		return nil, nil

	case TxStaging, TxVerifying:
		// Interrupted: staged files may exist but commit was never performed.
		// The active slot was NOT changed, so the system is still on the
		// pre-update slot and is safe to operate normally.
		log := NewAuditLog(opts.dataDir())
		reason := fmt.Sprintf("interrupted in state %s (detected at boot)", tx.State)
		abortTx(opts.dataDir(), tx, log, reason)
		return &RecoveryResult{
			Found:  true,
			TxID:   tx.ID,
			State:  tx.State,
			Action: "aborted",
		}, nil

	default:
		return nil, fmt.Errorf("unknown tx state %q in recovery", tx.State)
	}
}

// RollbackLastUpdate reverts the active slot to the pre-update slot recorded
// in the most recently committed transaction's health snapshot.
// It is a no-op when no committed transaction record exists.
func RollbackLastUpdate(opts Options) (string, error) {
	tx, err := LoadTx(opts.dataDir())
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("no transaction record found")
	}
	if err != nil {
		return "", err
	}
	if tx.State != TxCommitted {
		return "", fmt.Errorf("last transaction state is %q; nothing to roll back", tx.State)
	}
	if tx.Health == nil {
		return "", fmt.Errorf("no health snapshot in transaction; cannot determine previous slot")
	}
	prevSlot := tx.Health.ActiveSlot
	if err := writeActiveSlot(opts.dataDir(), prevSlot); err != nil {
		return "", fmt.Errorf("write active slot: %w", err)
	}

	log := NewAuditLog(opts.dataDir())
	log.Log(tx.ID, "tx.rolled_back",
		fmt.Sprintf("reverted from slot %s to %s", tx.TargetSlot, prevSlot))

	return prevSlot, nil
}
