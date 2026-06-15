package ctlsock_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/ctlsock"
)

// stubHandler is a minimal Handler for testing.
type stubHandler struct {
	services []ctlsock.ServiceInfo
}

func (h *stubHandler) ListServices() []ctlsock.ServiceInfo { return h.services }
func (h *stubHandler) ServiceStatus(name string) (ctlsock.ServiceInfo, bool) {
	for _, s := range h.services {
		if s.Name == name {
			return s, true
		}
	}
	return ctlsock.ServiceInfo{}, false
}
func (h *stubHandler) StartService(name string) error   { return nil }
func (h *stubHandler) StopService(name string) error    { return nil }
func (h *stubHandler) RestartService(name string) error { return nil }
func (h *stubHandler) ServiceLogs(name string, n int) ([]string, error) {
	return []string{fmt.Sprintf("log line from %s", name)}, nil
}
func (h *stubHandler) EnableService(name string) error  { return nil }
func (h *stubHandler) DisableService(name string) error { return nil }
func (h *stubHandler) Shutdown(reboot bool) error       { return nil }

func startTestServer(t *testing.T) (socketPath string, stop func()) {
	t.Helper()
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	handler := &stubHandler{
		services: []ctlsock.ServiceInfo{
			{Name: "gateway", State: "running", Restarts: 0, Since: "2026-01-01T00:00:00Z"},
			{Name: "nura-agent", State: "ready", Restarts: 1, Since: "2026-01-01T00:01:00Z"},
		},
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	srv := ctlsock.NewServer(sockPath, handler, log)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ctx)
	}()

	// Wait for the socket to be created.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	return sockPath, func() {
		cancel()
		<-errCh
	}
}

func TestServerList(t *testing.T) {
	sockPath, stop := startTestServer(t)
	defer stop()

	client := ctlsock.NewClient(sockPath)
	resp, err := client.Send(ctlsock.Request{Command: ctlsock.CmdList})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got error: %s", resp.Error)
	}
	if len(resp.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(resp.Services))
	}
}

func TestServerStatus(t *testing.T) {
	sockPath, stop := startTestServer(t)
	defer stop()

	client := ctlsock.NewClient(sockPath)
	resp, err := client.Send(ctlsock.Request{Command: ctlsock.CmdStatus, Service: "gateway"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got error: %s", resp.Error)
	}
	if resp.Service == nil {
		t.Fatal("expected service data in response")
	}
	if resp.Service.Name != "gateway" {
		t.Errorf("expected gateway, got %s", resp.Service.Name)
	}
}

func TestServerStatusUnknown(t *testing.T) {
	sockPath, stop := startTestServer(t)
	defer stop()

	client := ctlsock.NewClient(sockPath)
	resp, err := client.Send(ctlsock.Request{Command: ctlsock.CmdStatus, Service: "no-such-svc"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.OK {
		t.Fatal("expected ok=false for unknown service")
	}
}

func TestServerLogs(t *testing.T) {
	sockPath, stop := startTestServer(t)
	defer stop()

	client := ctlsock.NewClient(sockPath)
	resp, err := client.Send(ctlsock.Request{Command: ctlsock.CmdLogs, Service: "gateway", Lines: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got: %s", resp.Error)
	}
	if len(resp.Logs) == 0 {
		t.Error("expected at least one log line")
	}
}

func TestServerUnknownCommand(t *testing.T) {
	sockPath, stop := startTestServer(t)
	defer stop()

	client := ctlsock.NewClient(sockPath)
	resp, err := client.Send(ctlsock.Request{Command: "bogus"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.OK {
		t.Fatal("expected ok=false for unknown command")
	}
}
