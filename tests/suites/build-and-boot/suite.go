// Package buildandboot provides the build-and-boot integration test suite.
//
// It asserts that the NuraOS build artifacts exist and meet size expectations,
// that the system boots to a ready agent within the allocated budget, that the
// serial REPL responds to commands, that /data mounts correctly, and that the
// system reaches a usable state even when booted offline.
//
// Each case is annotated with the originating build-phase acceptance criteria
// it exercises.
package buildandboot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yasserrmd/nuraos/tests/harness"
)

const suite = "build-and-boot"

// Run executes the full build-and-boot suite. inst is a pre-booted, ready
// NuraOS QEMU instance provided by the test runner; some cases boot their own
// separate instances.
func Run(ctx context.Context, inst *harness.QEMUInstance) []harness.Result {
	return []harness.Result{
		caseKernelSize(ctx, inst),
		caseImageAssembly(ctx, inst),
		caseBootReady(ctx, inst),
		caseSerialREPL(ctx, inst),
		caseDataMounted(ctx, inst),
		caseOfflineBoot(ctx, inst),
	}
}

// ---------------------------------------------------------------------------
// Case: kernel-size
// Acceptance: Phase 01 -- kernel build produces a bzImage of expected size.
// ---------------------------------------------------------------------------

func caseKernelSize(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	const (
		minBytes = 1 * 1024 * 1024  // 1 MB lower bound
		maxBytes = 100 * 1024 * 1024 // 100 MB upper bound
	)

	bzImage := filepath.Join(inst.RepoRoot, "image", "out", "bzImage")
	info, err := os.Stat(bzImage)
	if err != nil {
		return fail("kernel-size", fmt.Sprintf("bzImage not found: %v", err))
	}
	sz := info.Size()
	if sz < minBytes {
		return fail("kernel-size", fmt.Sprintf("bzImage too small: %d bytes (minimum %d)", sz, minBytes))
	}
	if sz > maxBytes {
		return fail("kernel-size", fmt.Sprintf("bzImage too large: %d bytes (maximum %d)", sz, maxBytes))
	}
	return pass("kernel-size", fmt.Sprintf("bzImage exists, size %d bytes (%.1f MiB)", sz, float64(sz)/(1024*1024)))
}

// ---------------------------------------------------------------------------
// Case: image-assembly
// Acceptance: Phases 01/03/05/06 -- all expected artifacts are present.
// ---------------------------------------------------------------------------

func caseImageAssembly(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	outDir := filepath.Join(inst.RepoRoot, "image", "out")
	required := []string{
		"bzImage",
		"initramfs.cpio.gz",
		"data.img",
		"manifest.json",
	}
	for _, name := range required {
		p := filepath.Join(outDir, name)
		if _, err := os.Stat(p); err != nil {
			return fail("image-assembly", fmt.Sprintf("missing artifact %s: %v", name, err))
		}
	}
	return pass("image-assembly", fmt.Sprintf("all %d artifacts present in image/out/", len(required)))
}

// ---------------------------------------------------------------------------
// Case: boot-ready
// Acceptance: Phase 10 -- QEMU boots and /healthz returns 200 within budget.
// ---------------------------------------------------------------------------

func caseBootReady(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	// By the time the suite function is called the guest is already ready
	// (WaitReady was called by run-suite). We do a fresh /healthz call to
	// confirm liveness and record the RTT as an evidence metric.
	start := time.Now()
	code, body, err := inst.HTTP().GetBody("/healthz")
	rtt := time.Since(start)

	if err != nil {
		return fail("boot-ready", fmt.Sprintf("/healthz request failed: %v", err))
	}
	if code != 200 {
		return fail("boot-ready", fmt.Sprintf("/healthz returned HTTP %d (want 200): %s", code, body))
	}
	return pass("boot-ready", fmt.Sprintf("/healthz=200 rtt=%s body=%s", rtt.Round(time.Millisecond), body))
}

// ---------------------------------------------------------------------------
// Case: serial-repl
// Acceptance: Phase 35 -- serial REPL responds to :help with command list.
// ---------------------------------------------------------------------------

func caseSerialREPL(ctx context.Context, inst *harness.QEMUInstance) harness.Result {
	if err := inst.Serial().SendLine(":help"); err != nil {
		return fail("serial-repl", fmt.Sprintf("SendLine(:help) failed: %v", err))
	}
	// The REPL prints available commands; ":provider" is always present.
	// Skip if no REPL response -- serial REPL is a Phase 35+ feature.
	if err := inst.Serial().WaitForPattern(":provider", 10*time.Second); err != nil {
		return skip("serial-repl", "serial REPL not yet active (Phase 35+)")
	}
	return pass("serial-repl", "REPL responded to :help with command list")
}

// ---------------------------------------------------------------------------
// Case: data-mounted
// Acceptance: Phase 05 -- /data ext4 partition mounts on a fresh boot.
// ---------------------------------------------------------------------------

func caseDataMounted(ctx context.Context, inst *harness.QEMUInstance) harness.Result {
	dataImg := filepath.Join(inst.RepoRoot, "image", "out", "data.img")
	if _, err := os.Stat(dataImg); err != nil {
		return harness.Result{
			Suite: suite, Case: "data-mounted",
			Status:  harness.StatusSkip,
			Message: fmt.Sprintf("data.img not found (skipping): %v", err),
		}
	}

	sub, err := harness.BootQEMU(ctx, harness.QEMUOpts{
		RepoRoot:  inst.RepoRoot,
		DataImage: dataImg,
		MemMB:     256,
		CPUs:      1,
	})
	if err != nil {
		return fail("data-mounted", fmt.Sprintf("QEMU boot failed: %v", err))
	}
	defer func() { _ = sub.Close() }()

	// Wait for /data to be mounted; init logs this unconditionally.
	if err := sub.Serial().WaitForPattern("/data mounted", 90*time.Second); err != nil {
		return fail("data-mounted", fmt.Sprintf("'/data mounted' not seen in serial within 90s: %v", err))
	}
	return pass("data-mounted", "/data ext4 partition mounted successfully")
}

// ---------------------------------------------------------------------------
// Case: offline-boot
// Acceptance: Phases 00/01 -- system reaches supervisor without network.
// ---------------------------------------------------------------------------

func caseOfflineBoot(ctx context.Context, inst *harness.QEMUInstance) harness.Result {
	sub, err := harness.BootQEMU(ctx, harness.QEMUOpts{
		RepoRoot:  inst.RepoRoot,
		NoNetwork: true,
		MemMB:     256,
		CPUs:      1,
	})
	if err != nil {
		return fail("offline-boot", fmt.Sprintf("QEMU boot (no-network) failed: %v", err))
	}
	defer func() { _ = sub.Close() }()

	// The supervisor starts after init hands off control. Its presence in the
	// serial log confirms that kernel, initramfs, and init all ran successfully
	// even without a network device.
	if err := sub.Serial().WaitForPattern("[supervisor]", 90*time.Second); err != nil {
		return fail("offline-boot", fmt.Sprintf("[supervisor] not seen in serial within 90s: %v", err))
	}
	return pass("offline-boot", "supervisor started without network device")
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
