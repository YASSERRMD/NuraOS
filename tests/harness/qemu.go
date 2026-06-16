// Package harness provides the shared test infrastructure for NuraOS suites:
// QEMU boot management, serial console access, HTTP client, and result types.
package harness

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// QEMUOpts configures a NuraOS QEMU boot for testing.
type QEMUOpts struct {
	// RepoRoot is the absolute path to the NuraOS repository root (required).
	RepoRoot string
	// Kernel is the path to bzImage; defaults to image/out/bzImage.
	Kernel string
	// Initramfs is the path to initramfs.cpio.gz; defaults to image/out/initramfs.cpio.gz.
	Initramfs string
	// DataImage is the path to the /data ext4 image. If empty, /data is omitted.
	DataImage string
	// MemMB is RAM in megabytes (default: 512).
	MemMB int
	// CPUs is the vCPU count (default: 2).
	CPUs int
	// ExtraKernelArgs are appended verbatim to the kernel command line.
	ExtraKernelArgs string
	// NoNetwork omits the virtio-net device so the guest boots without any
	// network interface. APIPort and MetricsPort are still allocated but
	// port-forwarding is not configured. Used for offline-boot tests.
	NoNetwork bool
}

// QEMUInstance represents a running NuraOS QEMU VM.
type QEMUInstance struct {
	// APIPort is the host-side port forwarded to the guest HTTP gateway (port 8080).
	APIPort int
	// MetricsPort is the host-side port forwarded to the guest metrics endpoint (port 9090).
	MetricsPort int
	// SerialLogPath is the path to the file capturing serial console output.
	SerialLogPath string
	// StderrLogPath is the path to the file capturing QEMU process stderr.
	StderrLogPath string
	// RepoRoot is the NuraOS repository root used to boot this instance.
	RepoRoot string

	cmd    *exec.Cmd
	cancel context.CancelFunc
	tmpDir string
	serial *SerialClient
	http   *HTTPClient
	// exited is closed by a background reaper goroutine once the QEMU process
	// terminates. WaitReady selects on it to fast-fail on an early exit, and
	// Close waits on it instead of calling cmd.Wait() a second time.
	exited chan struct{}
}

// BootQEMU starts a NuraOS VM in QEMU and returns a handle to it.
//
// The serial console uses QEMU's file chardev backend (-serial file:PATH) so
// ALL guest bytes -- including early decompressor and earlyprintk output -- are
// written directly to disk with no connection race. The harness reads the file
// asynchronously via SerialClient.WaitForPattern.
// The caller must call Close() when the test is complete.
func BootQEMU(ctx context.Context, opts QEMUOpts) (*QEMUInstance, error) {
	if opts.RepoRoot == "" {
		return nil, fmt.Errorf("QEMUOpts.RepoRoot is required")
	}
	if opts.MemMB == 0 {
		opts.MemMB = 512
	}
	if opts.CPUs == 0 {
		opts.CPUs = 2
	}
	if opts.Kernel == "" {
		opts.Kernel = filepath.Join(opts.RepoRoot, "image", "out", "bzImage")
	}
	if opts.Initramfs == "" {
		opts.Initramfs = filepath.Join(opts.RepoRoot, "image", "out", "initramfs.cpio.gz")
	}

	apiPort, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("allocating API port: %w", err)
	}
	metricsPort, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("allocating metrics port: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "nuraos-test-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	serialLog := filepath.Join(tmpDir, "serial.log")
	qemuStderrPath := filepath.Join(tmpDir, "qemu.stderr")

	// console=ttyS0 routes printk to COM1 (the QEMU serial backend).
	// earlycon and earlyprintk enable output before console_init() so we
	// capture any early panic.  nokaslr makes boot deterministic; panic=5
	// forces a clean exit on panic so the harness detects boot failure fast.
	kernelArgs := "console=ttyS0,115200 earlycon=uart8250,io,0x3f8,115200n8 earlyprintk=serial,ttyS0,115200 nokaslr panic=5 loglevel=7"
	if opts.ExtraKernelArgs != "" {
		kernelArgs += " " + opts.ExtraKernelArgs
	}

	// Build QEMU argument list.
	// Machine: q35 matches the project's run-qemu.sh and is better supported
	// under TCG than the older i440fx (pc) machine type.
	// Accel: pure TCG (software emulation) avoids nested-KVM compatibility
	// issues on GitHub Actions Azure runners (Hyper-V → KVM → guest is three
	// levels deep and has known MSR/CPUID gaps). The project's own run-qemu.sh
	// uses accel=tcg; we follow that choice.
	// CPU: qemu64 is QEMU's own x86_64 model tuned for TCG -- it exposes all
	// required x86_64 features without exotic extensions (LA57, AVX-512, etc.)
	// that cause failures under nested virtualisation.
	// -serial file:PATH writes all ttyS0 bytes directly to disk the moment the
	// guest writes them -- no connection race, no buffering, no lost bytes.
	// -no-reboot converts panic emergency_restart into a clean QEMU exit so
	// the harness fast-fails on boot panic.
	args := []string{
		"-machine", "q35,accel=tcg",
		"-cpu", "qemu64",
		"-m", fmt.Sprintf("%dM", opts.MemMB),
		"-smp", fmt.Sprintf("%d", opts.CPUs),
		"-display", "none",
		"-serial", fmt.Sprintf("file:%s", serialLog),
		"-device", "virtio-rng-pci",
		// NOTE: do NOT add "-d int" here. Under TCG it logs every interrupt the
		// guest takes -- once the kernel boots and the 250Hz timer fires on all
		// CPUs that is megabytes of stderr per second, which starves the VM and
		// makes QEMU exit before /healthz ever comes up (the guest boots fine
		// without it -- verified locally). cpu_reset/guest_errors are low-volume
		// and safe if early-boot diagnostics are ever needed again.
		"-no-reboot",
		"-kernel", opts.Kernel,
		"-initrd", opts.Initramfs,
		"-append", kernelArgs,
	}
	if !opts.NoNetwork {
		args = append(args,
			"-netdev", fmt.Sprintf("user,id=net0,hostfwd=tcp::%d-:8080,hostfwd=tcp::%d-:9090",
				apiPort, metricsPort),
			"-device", "virtio-net-pci,netdev=net0",
		)
	}
	if opts.DataImage != "" {
		args = append(args,
			"-drive", fmt.Sprintf("file=%s,format=raw,if=virtio,cache=writeback", opts.DataImage),
		)
	}

	vmCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(vmCtx, "qemu-system-x86_64", args...)

	qemuStderr, err := os.Create(qemuStderrPath)
	if err != nil {
		cancel()
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("creating qemu stderr log: %w", err)
	}
	cmd.Stderr = qemuStderr

	if err := cmd.Start(); err != nil {
		_ = qemuStderr.Close()
		cancel()
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("starting qemu-system-x86_64: %w", err)
	}
	_ = qemuStderr.Close()

	// Background reaper: a single goroutine owns cmd.Wait() so the process is
	// reaped exactly once. It closes `exited` on termination, giving WaitReady a
	// reliable early-exit signal and Close a way to wait without a second Wait().
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()

	// Wait briefly for QEMU to create the serial log file before starting the
	// poll loop.  With file backend QEMU creates the file at startup (before
	// the VM begins executing), so this usually completes in milliseconds.
	if err := waitForFile(serialLog, 5*time.Second); err != nil {
		cancel()
		<-exited
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("waiting for serial log file: %w", err)
	}

	serial := newSerialClient(serialLog)

	return &QEMUInstance{
		APIPort:       apiPort,
		MetricsPort:   metricsPort,
		SerialLogPath: serialLog,
		StderrLogPath: qemuStderrPath,
		RepoRoot:      opts.RepoRoot,
		cmd:           cmd,
		cancel:        cancel,
		tmpDir:        tmpDir,
		serial:        serial,
		http: &HTTPClient{
			BaseURL: fmt.Sprintf("http://127.0.0.1:%d", apiPort),
		},
		exited: exited,
	}, nil
}

// Serial returns the serial console client for this VM instance.
func (q *QEMUInstance) Serial() *SerialClient { return q.serial }

// HTTP returns the HTTP client bound to the guest gateway port.
func (q *QEMUInstance) HTTP() *HTTPClient { return q.http }

// Close kills the QEMU process, stops the serial poll loop, and removes all
// temporary files created for this instance.
func (q *QEMUInstance) Close() error {
	q.cancel()
	if q.serial != nil {
		q.serial.close()
	}
	// The background reaper owns cmd.Wait(); wait for it to finish rather than
	// calling Wait() a second time (which would race / error).
	if q.exited != nil {
		<-q.exited
	}
	return os.RemoveAll(q.tmpDir)
}

// waitForFile polls until the file at path appears or timeout elapses.
func waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("file %s did not appear within %s", path, timeout)
}
