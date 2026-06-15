package eventbus

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"time"
)

// SocketPath is the Unix domain socket for the event bus.
const SocketPath = "/run/nura-events.sock"

// Server exposes a Bus over a Unix domain socket.
//
// Protocol (newline-delimited JSON):
//
//	Client connects and sends exactly one JSON line.
//	Subscribe: {"subscribe":true}
//	           Server streams future events as JSON lines until disconnect.
//	Publish:   <Event JSON>
//	           Server broadcasts the event and closes the connection.
//
// Socket permissions are 0644 so any local process can subscribe or publish.
// (Publishers and subscribers are trusted because they run in the same VM.)
type Server struct {
	path string
	bus  *Bus
	log  *slog.Logger
}

// NewServer creates a Server that will listen on path.
func NewServer(path string, bus *Bus, log *slog.Logger) *Server {
	return &Server{path: path, bus: bus, log: log}
}

// Serve listens for connections until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	_ = os.Remove(s.path)
	ln, err := net.Listen("unix", s.path)
	if err != nil {
		return err
	}
	if err := os.Chmod(s.path, 0644); err != nil {
		ln.Close()
		return err
	}
	if s.log != nil {
		s.log.Info("eventbus socket ready", "path", s.path)
	}

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if s.log != nil {
				s.log.Warn("eventbus: accept error", "err", err)
			}
			continue
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	// All connections must send their first message within 5 s.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return
	}
	line := scanner.Bytes()

	// Check whether this is a subscribe request.
	var maybeSubscribe struct {
		Subscribe bool `json:"subscribe"`
	}
	if json.Unmarshal(line, &maybeSubscribe) == nil && maybeSubscribe.Subscribe {
		s.serveSubscriber(ctx, conn)
		return
	}

	// Otherwise treat it as a publish.
	var ev Event
	if err := json.Unmarshal(line, &ev); err != nil {
		return
	}
	s.bus.Publish(ev)
}

func (s *Server) serveSubscriber(ctx context.Context, conn net.Conn) {
	// No read deadline after subscription; the client drives disconnect.
	_ = conn.SetReadDeadline(time.Time{})

	ch, cancel := s.bus.Subscribe(0)
	defer cancel()

	enc := json.NewEncoder(conn)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := enc.Encode(ev); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
