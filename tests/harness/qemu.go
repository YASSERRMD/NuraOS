// Package harness provides the shared test infrastructure for NuraOS suites:
// QEMU boot management, serial console access, HTTP client, and result types.
package harness

import (
	"context"
	"fmt"
	"net"
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
	// MemMB is RAM in megabytes (default: 256).
	MemMB int
	// CPUs is the vCPU count (default: 1).
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

	cmd        *exec.Cmd
	cancel     context.CancelFunc
	tmpDir     string
	serialConn net.Conn
	serial     *SerialClient
	http       *HTTPClient
}

// BootQEMU starts a NuraOS VM in QEMU and returns a handle to it.
//
// The serial console is connected via a UNIX socket so the harness can
// read boot output and send REPL commands without fixed sleeps.
// The caller must call Close() when the test is complete.
func BootQEMU(ctx context.Context, opts QEMUOpts) (*QEMUInstance, error) {
	if opts.RepoRoot == "" {
		return nil, fmt.Errorf("QEMUOpts.RepoRoot is required")
	}
	if opts.MemMB == 0 {
		opts.MemMB = 256
	}
	if opts.CPUs == 0 {
		opts.CPUs = 1
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

	// earlyprintk=serial,ttyS0,115200: enables very-early console output before
	// the regular 8250 driver initialises, so kernel panics that occur before
	// console_init() are captured in the serial log.
	kernelArgs := "console=ttyS0,115200 earlyprintk=serial,ttyS0,115200 panic=5 loglevel=7"
	if opts.ExtraKernelArgs != "" {
		kernelArgs += " " + opts.ExtraKernelArgs
	}

	// Build QEMU argument list. We use -display none so QEMU runs headlessly.
	// -serial file:PATH: QEMU writes serial output directly to the log file,
	// bypassing the Unix socket mechanism entirely. This guarantees every byte
	// is captured even if the kernel panics before console_init(), and removes
	// any timing dependency on socket client connection.
	// virtio-rng-pci: provides hardware entropy so the guest CSPRNG seeds
	// immediately and gateway startup is not delayed by entropy starvation.
	args := []string{
		"-machine", "q35,accel=kvm:tcg",
		"-cpu", "host",
		"-m", fmt.Sprintf("%dM", opts.MemMB),
		"-smp", fmt.Sprintf("%d", opts.CPUs),
		"-display", "none",
		"-object", "rng-builtin,id=rng0",
		"-device", "virtio-rng-pci,rng=rng0",
		"-serial", "file:" + serialLog,
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
		cancel()
		_ = qemuStderr.Close()
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("starting qemu-system-x86_64: %w", err)
	}

	// QEMU writes serial output directly to serialLog via "-serial file:".
	// No socket connection or handshake is needed; the file is created by QEMU
	// when the first byte is written.

	return &QEMUInstance{
		APIPort:       apiPort,
		MetricsPort:   metricsPort,
		SerialLogPath: serialLog,
		StderrLogPath: qemuStderrPath,
		RepoRoot:      opts.RepoRoot,
		cmd:           cmd,
		cancel:        cancel,
		tmpDir:        tmpDir,
		http: &HTTPClient{
			BaseURL: fmt.Sprintf("http://127.0.0.1:%d", apiPort),
		},
	}, nil
}

// Serial returns the serial console client for this VM instance.
func (q *QEMUInstance) Serial() *SerialClient { return q.serial }

// HTTP returns the HTTP client bound to the guest gateway port.
func (q *QEMUInstance) HTTP() *HTTPClient { return q.http }

// Close kills the QEMU process, disconnects the serial client, and removes
// all temporary files created for this instance.
func (q *QEMUInstance) Close() error {
	q.cancel()
	if q.serial != nil {
		q.serial.close()
	}
	if q.serialConn != nil {
		_ = q.serialConn.Close()
	}
	_ = q.cmd.Wait()
	return os.RemoveAll(q.tmpDir)
}

// waitForSocket polls until the UNIX socket at path appears or timeout elapses.
// It uses short-interval polling rather than a fixed sleep.
func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("serial socket %s did not appear within %s", path, timeout)
}
