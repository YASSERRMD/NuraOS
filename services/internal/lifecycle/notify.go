package lifecycle

import (
	"bufio"
	"io"
	"os"
	"strings"
)

// notifyListener reads lines from r and closes ready when "READY=1" is seen.
// It runs in a goroutine and exits when r is closed.
func notifyListener(r io.Reader, ready chan<- struct{}) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "READY=1" {
			select {
			case ready <- struct{}{}:
			default:
			}
			return
		}
	}
}

// newNotifyPipe creates a pipe for the notify protocol.
// The write end (w) is passed to the child via NOTIFY_FD.
// The read end (r) is monitored by notifyListener.
func newNotifyPipe() (r, w *os.File, err error) {
	r, w, err = os.Pipe()
	return
}
