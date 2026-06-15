package harness

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
)

// RunContext holds stable identifiers shared by all results in one suite run.
type RunContext struct {
	// RunID is a random 16-char hex string unique to this invocation.
	RunID string
	// CommitSHA is the HEAD commit hash read from git at run start.
	CommitSHA string
}

// NewRunContext creates a RunContext by generating a random run ID and
// reading the current HEAD commit from git in repoRoot.
func NewRunContext(repoRoot string) (*RunContext, error) {
	id, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("generating run ID: %w", err)
	}
	sha := gitHead(repoRoot)
	return &RunContext{RunID: id, CommitSHA: sha}, nil
}

// Apply stamps r with the run's RunID and CommitSHA.
func (rc *RunContext) Apply(r *Result) {
	r.RunID = rc.RunID
	r.CommitSHA = rc.CommitSHA
}

// randomHex returns n random bytes encoded as a lowercase hex string.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// gitHead returns the HEAD commit SHA from the repo at root.
// Returns "unknown" on any error so a missing git binary never blocks a run.
func gitHead(root string) string {
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
