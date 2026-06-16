package harness

import (
	"bytes"
	"fmt"
	"os"
	"sync"
	"time"
)

// SerialClient captures and searches serial console output written by QEMU
// to a file via the -serial file:PATH chardev backend. Polling the file
// avoids any race between harness connection timing and early kernel output:
// QEMU writes bytes the moment the guest generates them, regardless of when
// the harness starts reading.
type SerialClient struct {
	logPath   string
	mu        sync.RWMutex
	buf       bytes.Buffer
	closeOnce sync.Once
	stopCh    chan struct{}
	doneCh    chan struct{}
}

// newSerialClient starts a background goroutine that tails logPath, appending
// new bytes to the in-memory buffer as QEMU writes them.
func newSerialClient(logPath string) *SerialClient {
	s := &SerialClient{
		logPath: logPath,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	go s.pollLoop()
	return s
}

// pollLoop reads new bytes appended to the serial log file every 100 ms.
func (s *SerialClient) pollLoop() {
	defer close(s.doneCh)
	var offset int64
	for {
		select {
		case <-s.stopCh:
			// Final read to capture any bytes written since the last poll.
			s.appendFrom(offset)
			return
		case <-time.After(100 * time.Millisecond):
		}
		offset = s.appendFrom(offset)
	}
}

// appendFrom reads bytes at offset from the log file into the buffer.
func (s *SerialClient) appendFrom(offset int64) int64 {
	data, err := os.ReadFile(s.logPath)
	if err != nil || int64(len(data)) <= offset {
		return offset
	}
	s.mu.Lock()
	s.buf.Write(data[offset:])
	s.mu.Unlock()
	return int64(len(data))
}

// SendLine is not supported with the file chardev backend (ttyS0 is write-only
// from the host side). Tests that require REPL writes must skip themselves.
func (s *SerialClient) SendLine(_ string) error {
	return fmt.Errorf("SendLine: serial uses file backend; host writes not supported")
}

// CanWrite reports whether the serial client supports sending commands.
func (s *SerialClient) CanWrite() bool { return false }

// WaitForPattern blocks until pattern appears in the serial log or timeout elapses.
func (s *SerialClient) WaitForPattern(pattern string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	needle := []byte(pattern)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		found := bytes.Contains(s.buf.Bytes(), needle)
		s.mu.RUnlock()
		if found {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("pattern %q not found in serial output within %s", pattern, timeout)
}

// Snapshot returns a copy of the serial log captured so far.
func (s *SerialClient) Snapshot() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]byte, s.buf.Len())
	copy(out, s.buf.Bytes())
	return out
}

// close stops the poll loop and waits for it to exit.
func (s *SerialClient) close() {
	s.closeOnce.Do(func() { close(s.stopCh) })
	<-s.doneCh
}
