// Package storage provides the storage integration test suite (T10).
//
// It asserts that the /data ext4 image artifact exists, that it mounts
// correctly when supplied to a fresh QEMU boot, and that the models directory
// inside /data is reachable via the gateway /models endpoint.
package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yasserrmd/nuraos/tests/harness"
)

const suite = "storage"

// Run executes all storage cases against the pre-booted, ready instance.
func Run(ctx context.Context, inst *harness.QEMUInstance) []harness.Result {
	return []harness.Result{
		caseDataImgExists(ctx, inst),
		caseDataMountCheck(ctx, inst),
		caseModelsDirAccessible(ctx, inst),
	}
}

// ---------------------------------------------------------------------------
// Case: data-img-exists
// Acceptance: Phase 05 -- image/out/data.img exists.
// ---------------------------------------------------------------------------

func caseDataImgExists(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	dataImg := filepath.Join(inst.RepoRoot, "image", "out", "data.img")
	info, err := os.Stat(dataImg)
	if err != nil {
		return fail("data-img-exists", fmt.Sprintf("data.img not found: %v", err))
	}
	return pass("data-img-exists", fmt.Sprintf("data.img found (%d bytes)", info.Size()))
}

// ---------------------------------------------------------------------------
// Case: data-mount-check
// Acceptance: Phase 05 -- boot with DataImage, verify /data mounted in serial.
// ---------------------------------------------------------------------------

func caseDataMountCheck(ctx context.Context, inst *harness.QEMUInstance) harness.Result {
	dataImg := filepath.Join(inst.RepoRoot, "image", "out", "data.img")
	if _, err := os.Stat(dataImg); err != nil {
		return skip("data-mount-check", fmt.Sprintf("data.img absent (skipping boot test): %v", err))
	}

	sub, err := harness.BootQEMU(ctx, harness.QEMUOpts{
		RepoRoot:  inst.RepoRoot,
		DataImage: dataImg,
		MemMB:     256,
		CPUs:      1,
	})
	if err != nil {
		return fail("data-mount-check", fmt.Sprintf("QEMU boot with DataImage failed: %v", err))
	}
	defer func() { _ = sub.Close() }()

	if err := sub.Serial().WaitForPattern("/data mounted", 90*time.Second); err != nil {
		return fail("data-mount-check",
			fmt.Sprintf("'/data mounted' not seen in serial within 90s: %v", err))
	}
	return pass("data-mount-check", "/data ext4 partition mounted successfully on fresh boot")
}

// ---------------------------------------------------------------------------
// Case: models-dir-accessible
// Acceptance: Phase 39 -- GET /models returns 200 (reads /data/models).
// ---------------------------------------------------------------------------

func caseModelsDirAccessible(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/models")
	if err != nil {
		return fail("models-dir-accessible", fmt.Sprintf("GET /models error: %v", err))
	}
	if code != 200 {
		return fail("models-dir-accessible",
			fmt.Sprintf("GET /models returned %d (want 200): %s", code, body))
	}
	return pass("models-dir-accessible", fmt.Sprintf("GET /models=200; /data/models directory reachable"))
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
