// Package sockact implements socket activation for NuraOS service units.
//
// The manager pre-opens a TCP or Unix domain socket and waits for the first
// client connection. On first connection, it starts the lazily-activated
// service and hands off the listening socket via the standard LISTEN_FDS /
// LISTEN_PID convention (compatible with systemd socket activation).
//
// An idle monitor stops a running service when there has been no connection
// activity for a configurable duration. On the next connection the service is
// started again.
package sockact

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Holder pre-opens a listening socket and exposes it for passing to a child
// process via LISTEN_FDS. It also tracks connection activity for idle-stop.
type Holder struct {
	network string
	address string
	ln      net.Listener
	mu      sync.Mutex

	// lastActivity is the Unix nanosecond timestamp of the last accepted
	// connection. Updated atomically.
	lastActivity atomic.Int64
	// connectionCount is the total number of connections accepted.
	connectionCount atomic.Int64
}

// NewHolder creates and binds a listener on network/address.
// The caller must call Close when done.
func NewHolder(network, address string) (*Holder, error) {
	ln, err := net.Listen(network, address)
	if err != nil {
		return nil, fmt.Errorf("sockact bind %s %s: %w", network, address, err)
	}
	// Use the actual bound address (which resolves ":0" to the OS-assigned port).
	boundAddr := ln.Addr().String()
	h := &Holder{
		network: network,
		address: boundAddr,
		ln:      ln,
	}
	h.lastActivity.Store(time.Now().UnixNano())
	return h, nil
}

// File returns the underlying *net.TCPListener or *net.UnixListener as an
// *os.File so it can be passed to exec.Cmd.ExtraFiles.
// The returned file has an independent file descriptor; the caller is
// responsible for closing it.
func (h *Holder) File() (*fileCloser, error) {
	switch l := h.ln.(type) {
	case *net.TCPListener:
		f, err := l.File()
		if err != nil {
			return nil, err
		}
		return &fileCloser{f}, nil
	case *net.UnixListener:
		f, err := l.File()
		if err != nil {
			return nil, err
		}
		return &fileCloser{f}, nil
	default:
		return nil, fmt.Errorf("unsupported listener type %T", h.ln)
	}
}

// WaitFirstConnection blocks until a client connects to the pre-opened socket
// or ctx is cancelled. It returns immediately if a connection is already
// pending. The accepted connection is closed after recording activity
// (the real service will accept new connections via the inherited fd).
func (h *Holder) WaitFirstConnection(stopCh <-chan struct{}) error {
	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := h.ln.Accept()
		ch <- result{conn, err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			return fmt.Errorf("accept on activation socket: %w", r.err)
		}
		// Record activity and close the probe connection so the real service
		// handles subsequent connections via the inherited fd.
		h.touchActivity()
		r.conn.Close()
		return nil
	case <-stopCh:
		return fmt.Errorf("socket activation cancelled")
	}
}

// TouchActivity records the current time as last-seen activity.
func (h *Holder) TouchActivity() {
	h.touchActivity()
}

func (h *Holder) touchActivity() {
	h.lastActivity.Store(time.Now().UnixNano())
	h.connectionCount.Add(1)
}

// IdleFor returns how long the socket has been idle (no accepted connection).
func (h *Holder) IdleFor() time.Duration {
	last := time.Unix(0, h.lastActivity.Load())
	return time.Since(last)
}

// ConnectionCount returns the total connections seen.
func (h *Holder) ConnectionCount() int64 {
	return h.connectionCount.Load()
}

// Close releases the pre-opened socket.
func (h *Holder) Close() error {
	return h.ln.Close()
}

// Network and Address expose the binding for logging.
func (h *Holder) Network() string { return h.network }
func (h *Holder) Address() string { return h.address }
