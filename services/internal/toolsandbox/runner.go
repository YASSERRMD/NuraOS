package toolsandbox

import (
	"log/slog"

	"github.com/yasserrmd/nuraos/services/internal/cgroup"
	"github.com/yasserrmd/nuraos/services/internal/eventbus"
)

// Runner executes tool processes inside an OS-enforced sandbox.
// The zero value is not usable; use New.
type Runner struct {
	log   *slog.Logger
	bus   *eventbus.Bus
	cgMgr *cgroup.Manager
}

// New creates a Runner. bus may be nil. log must not be nil.
func New(log *slog.Logger, bus *eventbus.Bus) *Runner {
	return &Runner{
		log:   log,
		bus:   bus,
		cgMgr: cgroup.NewManager(),
	}
}
