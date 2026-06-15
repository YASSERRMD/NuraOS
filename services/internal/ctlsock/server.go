package ctlsock

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"
)

// Handler processes control requests from connected clients.
type Handler interface {
	// ListServices returns status for all known services.
	ListServices() []ServiceInfo
	// ServiceStatus returns status for a named service; ok=false if unknown.
	ServiceStatus(name string) (ServiceInfo, bool)
	// StartService requests that a service be started.
	StartService(name string) error
	// StopService requests that a service be stopped.
	StopService(name string) error
	// RestartService requests a stop then start.
	RestartService(name string) error
	// ServiceLogs returns the last n log lines for a service.
	ServiceLogs(name string, n int) ([]string, error)
	// EnableService marks a service as enabled in its unit file.
	EnableService(name string) error
	// DisableService marks a service as disabled in its unit file.
	DisableService(name string) error
	// Shutdown initiates a system-wide shutdown or reboot after ordered service stop.
	Shutdown(reboot bool) error
}

// Server listens on a Unix domain socket and dispatches requests to Handler.
type Server struct {
	path    string
	handler Handler
	log     *slog.Logger
}

// NewServer creates a Server that will listen on path.
func NewServer(path string, h Handler, log *slog.Logger) *Server {
	return &Server{path: path, handler: h, log: log}
}

// Serve starts accepting connections. It blocks until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	_ = os.Remove(s.path)
	ln, err := net.Listen("unix", s.path)
	if err != nil {
		return fmt.Errorf("control socket listen %s: %w", s.path, err)
	}
	// Restrict to root-only access: mode 0600.
	if err := os.Chmod(s.path, 0600); err != nil {
		ln.Close()
		return fmt.Errorf("chmod control socket: %w", err)
	}
	s.log.Info("control socket ready", "path", s.path)

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
			s.log.Warn("control socket accept error", "err", err)
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	dec := json.NewDecoder(bufio.NewReader(conn))
	enc := json.NewEncoder(conn)

	var req Request
	if err := dec.Decode(&req); err != nil {
		_ = enc.Encode(Response{OK: false, Error: "invalid request: " + err.Error()})
		return
	}

	resp := s.dispatch(req)
	_ = enc.Encode(resp)
}

func (s *Server) dispatch(req Request) Response {
	switch req.Command {
	case CmdList:
		return Response{OK: true, Services: s.handler.ListServices()}

	case CmdStatus:
		if req.Service == "" {
			return Response{OK: false, Error: "service name required"}
		}
		info, ok := s.handler.ServiceStatus(req.Service)
		if !ok {
			return Response{OK: false, Error: fmt.Sprintf("unknown service: %s", req.Service)}
		}
		return Response{OK: true, Service: &info}

	case CmdStart:
		if req.Service == "" {
			return Response{OK: false, Error: "service name required"}
		}
		if err := s.handler.StartService(req.Service); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, Message: "started " + req.Service}

	case CmdStop:
		if req.Service == "" {
			return Response{OK: false, Error: "service name required"}
		}
		if err := s.handler.StopService(req.Service); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, Message: "stopped " + req.Service}

	case CmdRestart:
		if req.Service == "" {
			return Response{OK: false, Error: "service name required"}
		}
		if err := s.handler.RestartService(req.Service); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, Message: "restarted " + req.Service}

	case CmdLogs:
		if req.Service == "" {
			return Response{OK: false, Error: "service name required"}
		}
		n := req.Lines
		if n <= 0 {
			n = 50
		}
		lines, err := s.handler.ServiceLogs(req.Service, n)
		if err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, Logs: lines}

	case CmdEnable:
		if req.Service == "" {
			return Response{OK: false, Error: "service name required"}
		}
		if err := s.handler.EnableService(req.Service); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, Message: "enabled " + req.Service}

	case CmdDisable:
		if req.Service == "" {
			return Response{OK: false, Error: "service name required"}
		}
		if err := s.handler.DisableService(req.Service); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, Message: "disabled " + req.Service}

	case CmdPoweroff:
		if err := s.handler.Shutdown(false); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, Message: "poweroff initiated"}

	case CmdReboot:
		if err := s.handler.Shutdown(true); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, Message: "reboot initiated"}

	default:
		return Response{OK: false, Error: fmt.Sprintf("unknown command: %s", req.Command)}
	}
}
