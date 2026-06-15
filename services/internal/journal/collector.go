package journal

import (
	"bufio"
	"io"
	"time"
)

// Collect reads lines from r (typically a service's stdout or stderr) and
// writes each as a journal record tagged with the given service name and pid.
// It returns when r reaches EOF or an error. Intended to run in a goroutine.
func Collect(r io.Reader, w *Writer, service string, pid int, pri Priority) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		_ = w.Write(Record{
			Time:    time.Now().UTC(),
			Service: service,
			PID:     pid,
			Pri:     pri,
			Message: line,
		})
	}
}

// CollectKmsg reads kernel messages from /dev/kmsg and writes them to w.
// /dev/kmsg records are in the format: "<priority>,<seq>,<timestamp>,<flags>;<message>".
// Simplified parse: extract the message and map priority to journal Priority.
// Returns when the file can no longer be read.
func CollectKmsg(path string, w *Writer) {
	f, err := openKmsg(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		pri, msg := parseKmsgLine(line)
		_ = w.Write(Record{
			Time:    time.Now().UTC(),
			Service: "kernel",
			Pri:     pri,
			Message: msg,
		})
	}
}
