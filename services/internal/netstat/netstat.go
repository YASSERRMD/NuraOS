// Package netstat reads per-interface network counters from /proc/net/dev.
// On non-Linux platforms all functions return empty results.
package netstat

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// DefaultProcPath is the standard location of /proc/net/dev.
const DefaultProcPath = "/proc/net/dev"

// IfaceStats holds transmit and receive counters for one network interface.
type IfaceStats struct {
	Name    string
	RxBytes uint64
	TxBytes uint64
	RxPkts  uint64
	TxPkts  uint64
	RxDrop  uint64
	TxDrop  uint64
	RxErrs  uint64
	TxErrs  uint64
}

// ReadStats reads /proc/net/dev and returns per-interface statistics.
// An error is returned only when the file cannot be opened or parsed;
// interfaces with missing fields are silently skipped.
func ReadStats() ([]IfaceStats, error) {
	return readFrom(DefaultProcPath)
}

func readFrom(path string) ([]IfaceStats, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("netstat: open %s: %w", path, err)
	}
	defer f.Close()

	var out []IfaceStats
	scanner := bufio.NewScanner(f)
	lineno := 0
	for scanner.Scan() {
		lineno++
		if lineno <= 2 {
			// Skip the two header lines.
			continue
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		s, err := parseLine(line)
		if err != nil {
			continue
		}
		out = append(out, s)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("netstat: scan %s: %w", path, err)
	}
	return out, nil
}

// parseLine parses one data line from /proc/net/dev.
// Format: <iface>: rx_bytes rx_pkts rx_errs rx_drop ... tx_bytes tx_pkts tx_errs tx_drop ...
// Column indices (0-based after the iface name):
//
//	0  rx_bytes  1  rx_pkts  2  rx_errs  3  rx_drop  (4 5 6 7 ignored)
//	8  tx_bytes  9  tx_pkts  10 tx_errs  11 tx_drop
func parseLine(line string) (IfaceStats, error) {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return IfaceStats{}, fmt.Errorf("no colon")
	}
	name := strings.TrimSpace(line[:colon])
	rest := strings.Fields(line[colon+1:])
	if len(rest) < 12 {
		return IfaceStats{}, fmt.Errorf("too few fields")
	}

	atoi := func(s string) uint64 {
		n, _ := strconv.ParseUint(s, 10, 64)
		return n
	}

	return IfaceStats{
		Name:    name,
		RxBytes: atoi(rest[0]),
		RxPkts:  atoi(rest[1]),
		RxErrs:  atoi(rest[2]),
		RxDrop:  atoi(rest[3]),
		TxBytes: atoi(rest[8]),
		TxPkts:  atoi(rest[9]),
		TxErrs:  atoi(rest[10]),
		TxDrop:  atoi(rest[11]),
	}, nil
}
