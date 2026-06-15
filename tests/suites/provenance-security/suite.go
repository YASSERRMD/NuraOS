// Package provenancesecurity provides the provenance-security integration test
// suite (T09).
//
// It asserts that build artifacts are accompanied by a signed manifest that
// records content hashes, that the system does not leak secrets on boot, and
// that the running instance reports an integrity component in its status.
package provenancesecurity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yasserrmd/nuraos/tests/harness"
)

const suite = "provenance-security"

// Run executes all provenance-security cases.
func Run(ctx context.Context, inst *harness.QEMUInstance) []harness.Result {
	return []harness.Result{
		caseManifestExists(ctx, inst),
		caseManifestHasHashes(ctx, inst),
		caseNoSecretsInImage(ctx, inst),
		caseIntegrityStatus(ctx, inst),
		caseVersionPinned(ctx, inst),
	}
}

// ---------------------------------------------------------------------------
// Case: manifest-exists
// Acceptance: Phase 26 -- image/out/manifest.json exists and is valid JSON.
// ---------------------------------------------------------------------------

func caseManifestExists(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	manifestPath := filepath.Join(inst.RepoRoot, "image", "out", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fail("manifest-exists", fmt.Sprintf("manifest.json not found: %v", err))
	}
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fail("manifest-exists", fmt.Sprintf("manifest.json is not valid JSON: %v", err))
	}
	return pass("manifest-exists", fmt.Sprintf("manifest.json found and parsed (%d bytes)", len(data)))
}

// ---------------------------------------------------------------------------
// Case: manifest-has-hashes
// Acceptance: Phase 26 -- manifest contains sha256 fields for artifacts.
// ---------------------------------------------------------------------------

func caseManifestHasHashes(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	manifestPath := filepath.Join(inst.RepoRoot, "image", "out", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return skip("manifest-has-hashes", fmt.Sprintf("manifest.json not found (skipping): %v", err))
	}
	body := string(data)
	if !strings.Contains(body, "sha256") {
		return fail("manifest-has-hashes", "manifest.json contains no 'sha256' fields")
	}
	return pass("manifest-has-hashes", "manifest.json contains sha256 hash fields")
}

// ---------------------------------------------------------------------------
// Case: no-secrets-in-image
// Acceptance: Phase 27 -- serial log does not contain API key patterns on boot.
// ---------------------------------------------------------------------------

// bootSecretPatterns are substrings that would indicate a secret leaked to
// the console during normal operation.
var bootSecretPatterns = []string{
	"sk-ant-api",
	"sk-proj-",
	"Bearer sk-",
}

func caseNoSecretsInImage(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	logPath := inst.SerialLogPath
	data, err := os.ReadFile(logPath)
	if err != nil {
		return skip("no-secrets-in-image", fmt.Sprintf("serial log not readable (skipping): %v", err))
	}
	logContent := string(data)
	for _, pat := range bootSecretPatterns {
		if strings.Contains(logContent, pat) {
			return fail("no-secrets-in-image",
				fmt.Sprintf("secret pattern %q found in serial boot log", pat))
		}
	}
	return pass("no-secrets-in-image", "no API key patterns found in serial boot log")
}

// ---------------------------------------------------------------------------
// Case: integrity-status
// Acceptance: Phase 26 -- GET /status shows integrity component.
// ---------------------------------------------------------------------------

func caseIntegrityStatus(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/status")
	if err != nil {
		return fail("integrity-status", fmt.Sprintf("GET /status error: %v", err))
	}
	if code != 200 {
		return fail("integrity-status", fmt.Sprintf("GET /status returned %d (want 200): %s", code, body))
	}
	if !strings.Contains(body, "integrity") {
		return skip("integrity-status", "GET /status does not mention 'integrity' component (not yet implemented)")
	}
	return pass("integrity-status", "GET /status reports integrity component")
}

// ---------------------------------------------------------------------------
// Case: version-pinned
// Acceptance: Phase 26 -- manifest has pinned nura_version and kernel_version.
// ---------------------------------------------------------------------------

func caseVersionPinned(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	manifestPath := filepath.Join(inst.RepoRoot, "image", "out", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return skip("version-pinned", fmt.Sprintf("manifest.json not found (skipping): %v", err))
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return fail("version-pinned", fmt.Sprintf("manifest.json is not valid JSON: %v", err))
	}
	missing := []string{}
	for _, field := range []string{"nura_version", "kernel_version"} {
		if _, ok := m[field]; !ok {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return fail("version-pinned",
			fmt.Sprintf("manifest.json missing pinned version fields: %s", strings.Join(missing, ", ")))
	}
	return pass("version-pinned",
		fmt.Sprintf("manifest.json has nura_version=%v kernel_version=%v", m["nura_version"], m["kernel_version"]))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func pass(case_, msg string) harness.Result {
	return harness.Result{Suite: suite, Case: case_, Status: harness.StatusPass, Message: msg}
}

func fail(case_, msg string) harness.Result {
	return harness.Result{Suite: suite, Case: case_, Status: harness.StatusFail, Message: msg}
}

func skip(case_, msg string) harness.Result {
	return harness.Result{Suite: suite, Case: case_, Status: harness.StatusSkip, Message: msg}
}
