package lifecycle

import (
	"fmt"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/ctlsock"
)

// CtlHandler adapts a Manager to the ctlsock.Handler interface so the
// control socket server can query and control services.
type CtlHandler struct {
	mgr     *Manager
	unitDir string
	plan    []*unitRef
}

type unitRef struct {
	name    string
	enabled bool
}

// NewCtlHandler wraps mgr for use as a control socket handler.
// plan records all unit names so enable/disable can be applied.
func NewCtlHandler(mgr *Manager, plan []string) *CtlHandler {
	refs := make([]*unitRef, len(plan))
	for i, name := range plan {
		refs[i] = &unitRef{name: name, enabled: true}
	}
	return &CtlHandler{mgr: mgr, plan: refs}
}

// ListServices implements ctlsock.Handler.
func (h *CtlHandler) ListServices() []ctlsock.ServiceInfo {
	snaps := h.mgr.AllStatuses()
	out := make([]ctlsock.ServiceInfo, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, toServiceInfo(s))
	}
	return out
}

// ServiceStatus implements ctlsock.Handler.
func (h *CtlHandler) ServiceStatus(name string) (ctlsock.ServiceInfo, bool) {
	snap, ok := h.mgr.Status(name)
	if !ok {
		return ctlsock.ServiceInfo{}, false
	}
	return toServiceInfo(snap), true
}

// StartService implements ctlsock.Handler.
func (h *CtlHandler) StartService(name string) error {
	return fmt.Errorf("start via nuractl not yet supported (use unit enables); service=%s", name)
}

// StopService implements ctlsock.Handler.
func (h *CtlHandler) StopService(name string) error {
	h.mgr.mu.Lock()
	run := h.mgr.procs[name]
	status := h.mgr.statuses[name]
	h.mgr.mu.Unlock()

	if run == nil {
		return fmt.Errorf("service %q is not running", name)
	}
	if status != nil {
		_ = status.transition(StateStopping, "nuractl stop")
	}
	run.stop()
	if status != nil {
		_ = status.transition(StateInactive, "stopped via nuractl")
	}
	return nil
}

// RestartService implements ctlsock.Handler.
func (h *CtlHandler) RestartService(name string) error {
	if err := h.StopService(name); err != nil {
		return err
	}
	return fmt.Errorf("restart via nuractl: service will restart via its policy; service=%s", name)
}

// ServiceLogs implements ctlsock.Handler.
// Phase 60 (journal) will wire real log access here; for now returns a placeholder.
func (h *CtlHandler) ServiceLogs(name string, n int) ([]string, error) {
	return []string{fmt.Sprintf("[journal not yet available - Phase 60 will wire logs for %s]", name)}, nil
}

// EnableService implements ctlsock.Handler.
func (h *CtlHandler) EnableService(name string) error {
	for _, ref := range h.plan {
		if ref.name == name {
			ref.enabled = true
			return nil
		}
	}
	return fmt.Errorf("unknown service: %s", name)
}

// DisableService implements ctlsock.Handler.
func (h *CtlHandler) DisableService(name string) error {
	for _, ref := range h.plan {
		if ref.name == name {
			ref.enabled = false
			return nil
		}
	}
	return fmt.Errorf("unknown service: %s", name)
}

func toServiceInfo(s StatusSnapshot) ctlsock.ServiceInfo {
	return ctlsock.ServiceInfo{
		Name:     s.Name,
		State:    s.State.String(),
		PID:      s.PID,
		Restarts: s.Restarts,
		Since:    s.Since.UTC().Format(time.RFC3339),
		Enabled:  true,
	}
}
