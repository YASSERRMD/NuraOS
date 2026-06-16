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
	// SerialLogPath is the path to the file capturing serial console output (ttyS0 unix socket).
	SerialLogPath string
	// EarlySerialLogPath is the path to the file capturing ttyS1 output written
	// directly by QEMU (no socket). Non-zero bytes here when SerialLogPath is
	// empty proves the unix socket is losing data; both empty proves the kernel
	// is not writing to any UART at all.
	EarlySerialLogPath string
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

	serialSock := filepath.Join(tmpDir, "serial.sock")
	serialLog := filepath.Join(tmpDir, "serial.log")
	earlySerialLog := filepath.Join(tmpDir, "early-serial.log")
	qemuStderrPath := filepath.Join(tmpDir, "qemu.stderr")

	// console=ttyS0 and console=ttyS1: write kernel messages to both the unix
	// socket serial (ttyS0) and the direct-file serial (ttyS1).  If ttyS1
	// captures data that ttyS0 does not, the unix socket backend is at fault.
	// If both are empty, the kernel is not writing to any UART at all.
	// earlycon=uart8250,io,0x3f8 and 0x2f8: register early consoles for COM1
	// and COM2 so we get output before the full 8250 driver initialises.
	// earlyprintk=serial,ttyS0 and ttyS1: legacy early-printk path for both.
	// nokaslr: disable KASLR so the kernel does not need early entropy from
	// RDRAND before the virtio-rng device is available. KASLR runs in the
	// decompressor phase, before any serial console is set up, so a failure
	// there produces zero serial output and looks identical to a silent hang.
	// panic=5: reboot after 5 s so -no-reboot can exit QEMU on a kernel panic.
	kernelArgs := "console=ttyS0,115200 console=ttyS1,115200" +
		" earlycon=uart8250,io,0x3f8,115200 earlycon=uart8250,io,0x2f8,115200" +
		" earlyprintk=serial,ttyS0,115200 earlyprintk=serial,ttyS1,115200" +
		" nokaslr panic=5 loglevel=7"
	if opts.ExtraKernelArgs != "" {
		kernelArgs += " " + opts.ExtraKernelArgs
	}

	// -machine q35,accel=kvm:tcg: Intel Q35 chipset. KVM is tried first (it is
	//   enabled on CI Azure runners via the Enable KVM workflow step); TCG is
	//   the fallback for environments without hardware virtualisation.  KVM
	//   boots the kernel ~100× faster than TCG and avoids TCG-specific
	//   emulation edge-cases that can cause silent early-boot failures.
	// -cpu qemu64: minimal, predictable 64-bit CPU; same as run-qemu.sh.
	// -serial unix:SOCK,server,nowait: ttyS0 via a UNIX domain socket so the
	//   harness can read boot output and send REPL commands (SerialClient).
	// -chardev file … -device isa-serial: ttyS1 direct-file serial backend.
	//   QEMU writes here without any socket; if this log has content while
	//   serial.log (ttyS0) is empty the unix socket backend is the culprit.
	// -d cpu_reset,guest_errors: QEMU debug events; output goes to qemu.stderr.
	// virtio-rng-pci: provides hardware entropy to the guest via the virtio
	//   bus, seeding the kernel CSPRNG early without relying on host RDRAND.
	args := []string{
		"-machine", "q35,accel=kvm:tcg",
		"-cpu", "qemu64",
		"-m", fmt.Sprintf("%dM", opts.MemMB),
		"-smp", fmt.Sprintf("%d", opts.CPUs),
		"-display", "none",
		"-serial", fmt.Sprintf("unix:%s,server,nowait", serialSock),
		"-chardev", fmt.Sprintf("file,id=earlyserial,path=%s", earlySerialLog),
		"-device", "isa-serial,chardev=earlyserial",
		"-object", "rng-builtin,id=rng0",
		"-device", "virtio-rng-pci,rng=rng0",
		"-d", "cpu_reset,guest_errors",
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

	// Redirect QEMU stderr → qemu.stderr (QEMU internal messages, CPU resets, etc.).
	// Serial output goes to the unix socket, not to QEMU's stdout/stderr.
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

	// Wait for QEMU to create the serial socket before connecting.
	if err := waitForSocket(serialSock, 10*time.Second); err != nil {
		cancel()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = qemuStderr.Close()
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("waiting for serial socket: %w", err)
	}

	conn, err := net.Dial("unix", serialSock)
	if err != nil {
		cancel()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = qemuStderr.Close()
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("connecting to serial socket: %w", err)
	}

	logFile, err := os.Create(serialLog)
	if err != nil {
		_ = conn.Close()
		cancel()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = qemuStderr.Close()
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("creating serial log: %w", err)
	}

	serial := newSerialClient(conn, logFile)

	return &QEMUInstance{
		APIPort:            apiPort,
		MetricsPort:        metricsPort,
		SerialLogPath:      serialLog,
		EarlySerialLogPath: earlySerialLog,
		StderrLogPath:      qemuStderrPath,
		RepoRoot:           opts.RepoRoot,
		cmd:           cmd,
		cancel:        cancel,
		tmpDir:        tmpDir,
		serialConn:    conn,
		serial:        serial,
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
