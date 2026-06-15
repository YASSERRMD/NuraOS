package sockact

import (
	"context"
	"log/slog"
	"time"
)

// IdleMonitor polls a Holder and calls onIdle when the socket has been idle
// for longer than timeout. It continues monitoring so that if the service
// restarts and activity resumes, subsequent idle periods also trigger onIdle.
func IdleMonitor(ctx context.Context, h *Holder, timeout time.Duration, log *slog.Logger, onIdle func()) {
	if timeout <= 0 {
		return
	}
	ticker := time.NewTicker(timeout / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if h.IdleFor() >= timeout {
				log.Info("socket idle timeout reached", "address", h.Address(), "idle", h.IdleFor().Round(time.Second))
				onIdle()
				// Reset the idle clock so we do not fire again immediately
				// after the service is stopped (no connections = still idle).
				h.touchActivity()
			}
		}
	}
}
