// Package update implements transactional A/B rootfs updates for NuraOS.
//
// Flow:
//
//  1. Stage: write the incoming image to a temp file under /data/update/staging/.
//  2. Verify: check SHA-256 and Ed25519 signature before touching any boot slot.
//  3. Snapshot: record the pre-update health state for rollback.
//  4. Commit: atomically rename the staged file into the inactive boot slot,
//     flip active-slot, write the audit log entry.
//  5. Recovery: on next boot, if a transaction JSON exists in state "staging"
//     or "verifying", the update was interrupted -- abort and clean up.
//
// The update audit log is separate from the journal; it lives at
// /data/update/audit.log and records every transaction event.
package update

// TxState is the lifecycle state of an update transaction.
type TxState string

const (
	TxStaging   TxState = "staging"
	TxVerifying TxState = "verifying"
	TxCommitted TxState = "committed"
	TxAborted   TxState = "aborted"
)

// Transaction is the durable record of a single update attempt, persisted to
// /data/update/tx.json. It survives power loss so interrupted updates can be
// detected and cleaned up on the next boot.
type Transaction struct {
	ID          string          `json:"id"`
	State       TxState         `json:"state"`
	TargetSlot  string          `json:"target_slot"`
	Source      string          `json:"source"`
	StagedPath  string          `json:"staged_path"`
	ExpectedSHA string          `json:"sha256"`
	StartedAt   string          `json:"started_at"`
	CommittedAt string          `json:"committed_at,omitempty"`
	AbortedAt   string          `json:"aborted_at,omitempty"`
	AbortReason string          `json:"abort_reason,omitempty"`
	Health      *HealthSnapshot `json:"health_snapshot,omitempty"`
}

// HealthSnapshot records the system state immediately before a commit.
// It is stored in the transaction so rollback can confirm what was healthy.
type HealthSnapshot struct {
	ActiveSlot      string   `json:"active_slot"`
	Timestamp       string   `json:"timestamp"`
	ServicesRunning []string `json:"services_running"`
}

// Options holds paths used by all update operations.
type Options struct {
	// DataDir is the persistent data root (default: /data).
	DataDir string
	// RootfsDir is the directory containing rootfs-a.ext4 and rootfs-b.ext4 (default: /boot).
	RootfsDir string
	// PubKey is the Ed25519 public key for image signature verification.
	// If empty, signature verification is skipped (not recommended for production).
	PubKey []byte
}

func (o Options) dataDir() string {
	if o.DataDir != "" {
		return o.DataDir
	}
	return "/data"
}

func (o Options) rootfsDir() string {
	if o.RootfsDir != "" {
		return o.RootfsDir
	}
	return "/boot"
}
