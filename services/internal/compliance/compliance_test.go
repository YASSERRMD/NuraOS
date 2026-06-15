package compliance_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/compliance"
)

// TestDefaultPolicyRejectsLocallyClassifiedCrossBorderRoute verifies the
// default policy blocks cross-border egress.
func TestDefaultPolicyRejectsLocallyClassifiedCrossBorderRoute(t *testing.T) {
	p := compliance.DefaultPolicy()
	if err := p.CheckRoute("anthropic", false); err == nil {
		t.Error("CheckRoute(anthropic) with AllowCrossBorder=false: expected error; got nil")
	}
}

// TestDefaultPolicyAllowsLocalRoute verifies local provider is always allowed.
func TestDefaultPolicyAllowsLocalRoute(t *testing.T) {
	p := compliance.DefaultPolicy()
	if err := p.CheckRoute("local", false); err != nil {
		t.Errorf("CheckRoute(local): unexpected error: %v", err)
	}
}

// TestSensitiveRouteBlockedOnCrossBorderProvider verifies sensitive turns are
// blocked on cross-border providers even when cross-border is globally allowed.
func TestSensitiveRouteBlockedOnCrossBorderProvider(t *testing.T) {
	p := compliance.DefaultPolicy()
	p.AllowCrossBorder = true // allow cross-border in general

	if err := p.CheckRoute("anthropic", true); err == nil {
		t.Error("CheckRoute(anthropic, sensitive=true): expected error; got nil (allow_sensitive=false)")
	}
}

// TestCrossBorderAllowedWhenPolicyPermits verifies non-sensitive turns route
// to cross-border when AllowCrossBorder=true.
func TestCrossBorderAllowedWhenPolicyPermits(t *testing.T) {
	p := compliance.DefaultPolicy()
	p.AllowCrossBorder = true

	if err := p.CheckRoute("anthropic", false); err != nil {
		t.Errorf("CheckRoute(anthropic, sensitive=false, allow_cross_border=true): unexpected error: %v", err)
	}
}

// TestUnknownProviderDefaultsCrossBorder verifies unlisted providers default
// to cross-border policy.
func TestUnknownProviderDefaultsCrossBorder(t *testing.T) {
	p := compliance.DefaultPolicy()
	pp := p.ProviderForName("unknown-provider")
	if pp.Residency != compliance.ResidencyCrossBorder {
		t.Errorf("unknown provider residency = %q; want cross-border", pp.Residency)
	}
	if pp.AllowSensitive {
		t.Error("unknown provider allow_sensitive should be false")
	}
}

// TestPolicySaveAndLoad verifies Save/Load round-trips correctly.
func TestPolicySaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	orig := compliance.DefaultPolicy()
	orig.AllowCrossBorder = true
	orig.RetentionDays = 30

	if err := compliance.Save(path, orig); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := compliance.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.AllowCrossBorder != orig.AllowCrossBorder {
		t.Errorf("AllowCrossBorder mismatch: got %v; want %v", loaded.AllowCrossBorder, orig.AllowCrossBorder)
	}
	if loaded.RetentionDays != 30 {
		t.Errorf("RetentionDays = %d; want 30", loaded.RetentionDays)
	}
}

// TestLoadDefaultWhenMissing verifies DefaultPolicy is returned for missing file.
func TestLoadDefaultWhenMissing(t *testing.T) {
	p, err := compliance.Load("/nonexistent-policy-xyz.json")
	if err != nil {
		t.Fatalf("Load missing file: unexpected error: %v", err)
	}
	if p.AllowCrossBorder {
		t.Error("default policy should have AllowCrossBorder=false")
	}
}

// TestAuditLogAppendAndReport verifies turn records are persisted and readable.
func TestAuditLogAppendAndReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	log := compliance.NewAuditLog(path)

	recs := []compliance.TurnRecord{
		{TurnID: "t1", Provider: "local", Residency: compliance.ResidencyLocal, Sensitive: false, At: time.Now()},
		{TurnID: "t2", Provider: "anthropic", Residency: compliance.ResidencyCrossBorder, Sensitive: false, CrossBorder: true, At: time.Now()},
	}
	for _, r := range recs {
		if err := log.Append(r); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	report, err := log.Report()
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(report) != 2 {
		t.Errorf("Report: %d records; want 2", len(report))
	}
	if report[1].CrossBorder != true {
		t.Error("second record CrossBorder should be true")
	}
}

// TestDeleteExpiredRemovesOldFiles verifies retention policy removes old data.
func TestDeleteExpiredRemovesOldFiles(t *testing.T) {
	dataDir := t.TempDir()
	// Create synthetic old session files.
	sessDir := filepath.Join(dataDir, "sessions")
	_ = os.MkdirAll(sessDir, 0755)

	// Write a file and back-date its mtime to 100 days ago.
	oldFile := filepath.Join(sessDir, "old-session.json")
	_ = os.WriteFile(oldFile, []byte(`{"id":"old"}`), 0644)
	oldTime := time.Now().Add(-100 * 24 * time.Hour)
	_ = os.Chtimes(oldFile, oldTime, oldTime)

	// Write a recent file.
	newFile := filepath.Join(sessDir, "new-session.json")
	_ = os.WriteFile(newFile, []byte(`{"id":"new"}`), 0644)

	res, err := compliance.DeleteExpired(dataDir, 90)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if res.SessionsDeleted != 1 {
		t.Errorf("SessionsDeleted = %d; want 1", res.SessionsDeleted)
	}
	// Old file should be gone.
	if _, err := os.Stat(oldFile); err == nil {
		t.Error("old session file still exists after deletion")
	}
	// New file should remain.
	if _, err := os.Stat(newFile); err != nil {
		t.Errorf("new session file was deleted: %v", err)
	}
}
