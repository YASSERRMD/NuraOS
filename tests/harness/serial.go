package harness

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// SerialClient provides read/write access to the VM serial console via the
// UNIX socket created by QEMU's -serial unix:PATH,server,nowait option.
type SerialClient struct {
	conn net.Conn
	log  *os.File
	mu   sync.RWMutex
	buf  bytes.Buffer
	done chan struct{}
}

// newSerialClient starts the background read loop that copies serial output
// into the in-memory buffer and the log file simultaneously.
func newSerialClient(conn net.Conn, log *os.File) *SerialClient {
	s := &SerialClient{
		conn: conn,
		log:  log,
		done: make(chan struct{}),
	}
	go s.readLoop()
	return s
}

// readLoop drains the serial socket byte-by-byte into the buffer and log file.
// It exits when the connection is closed (normal on VM shutdown).
func (s *SerialClient) readLoop() {
	defer close(s.done)
	r := bufio.NewReader(s.conn)
	for {
		b, err := r.ReadByte()
		if err != nil {
			if err != io.EOF {
				// Connection closed; expected on VM shutdown or harness teardown.
			}
			return
		}
		s.mu.Lock()
		s.buf.WriteByte(b)
		s.mu.Unlock()
		if s.log != nil {
			_, _ = s.log.Write([]byte{b})
		}
	}
}

// SendLine writes a single command line to the serial console. It appends
// a newline, which triggers the REPL to process the command.
func (s *SerialClient) SendLine(cmd string) error {
	_, err := s.conn.Write([]byte(cmd + "\n"))
	return err
}

// WaitForPattern polls the serial buffer until the pattern string appears or
// the timeout elapses. It uses short-interval polling rather than a fixed sleep.
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

// Snapshot returns a copy of the serial buffer contents captured so far.
func (s *SerialClient) Snapshot() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]byte, s.buf.Len())
	copy(out, s.buf.Bytes())
	return out
}

// close shuts down the read loop and closes the log file.
// It does NOT close the underlying connection (managed by QEMUInstance).
func (s *SerialClient) close() {
	if s.log != nil {
		_ = s.log.Close()
	}
	<-s.done
}
