package subcmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/yasserrmd/nuraos/services/internal/ctlsock"
	"github.com/yasserrmd/nuraos/services/internal/identity"
	"github.com/yasserrmd/nuraos/services/internal/journal"
	"github.com/yasserrmd/nuraos/services/internal/lifecycle"
	"github.com/yasserrmd/nuraos/services/internal/resolver"
	"github.com/yasserrmd/nuraos/services/internal/timesync"
	"github.com/yasserrmd/nuraos/services/internal/unit"
)

// defaultLogRatePerSec is the per-service log flood cap. Services that emit
// more than this many lines per second have the excess silently dropped.
const defaultLogRatePerSec = 200

// Run loads units from dir, resolves their order, and starts them in sequence
// using the lifecycle Manager. A control socket is exposed for nuractl.
// It blocks until SIGTERM/SIGINT, then performs ordered shutdown.
func Run(dir string) error {
	const dataDir = "/data"
	const journalDir = dataDir + "/journal"

	// Open journal first so the router can direct all log output there.
	jw, jwErr := journal.NewWriter(journalDir, journal.DefaultMaxSize)
	if jwErr != nil {
		jw = nil
	} else {
		defer jw.Close()
		limiter := journal.NewFloodLimiter(defaultLogRatePerSec)
		jw.SetLimiter(limiter)
		go journal.CollectKmsg("", jw)
	}

	// Severity-based routing: warnings+ to console, everything to journal.
	log := slog.New(journal.NewRouter(jw, os.Stdout, "nura-manager"))

	if jw != nil {
		log.Info("journal started", "dir", journalDir)
	} else {
		log.Warn("journal init failed; service logs will go to stdout", "err", jwErr, "dir", journalDir)
	}

	units, err := unit.LoadDir(dir)
	if err != nil {
		return fmt.Errorf("load units: %w", err)
	}
	if len(units) == 0 {
		log.Warn("no enabled units found", "dir", dir)
		return nil
	}

	plan, err := resolver.Resolve(units)
	if err != nil {
		return fmt.Errorf("dependency resolution: %w", err)
	}

	log.Info("nura-manager starting", "units", len(plan.Order))

	ctx, cancel := context.WithCancel(context.Background())

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigs
		log.Info("received signal", "signal", sig)
		cancel()
	}()

	// System identity: machine-id and hostname (set before starting services).
	machineID, idErr := identity.LoadOrCreate(dataDir)
	if idErr != nil {
		log.Warn("machine-id unavailable", "err", idErr)
		machineID = "unknown-machine"
	}
	if hostname, herr := identity.LoadHostname(dataDir, machineID); herr == nil {
		if sErr := identity.SetHostname(hostname); sErr != nil {
			log.Warn("set hostname failed", "err", sErr)
		} else {
			log.Info("identity applied", "hostname", hostname, "machine_id", machineID)
		}
	}

	// Time subsystem: RTC, timezone, optional NTP.
	timeMgr := timesync.NewManager(log)
	if err := timeMgr.Start(ctx, timesync.Config{
		NTPServer: os.Getenv("NURA_NTP_SERVER"),
	}); err != nil {
		log.Warn("time manager start error", "err", err)
	}

	// Optional remote forwarding: set NURA_FORWARD_URL to enable.
	if fwdURL := os.Getenv("NURA_FORWARD_URL"); fwdURL != "" && jw != nil {
		fwd := journal.NewForwarder(journalDir, journal.ForwardConfig{
			URL:         fwdURL,
			MinPriority: journal.PriWarning,
		})
		go fwd.Run(ctx)
		log.Info("log forwarding enabled", "url", fwdURL)
	}

	_ = timeMgr // available for future service injection
	mgr := lifecycle.NewManager(log, jw)

	// Start the control socket server so nuractl can query and control services.
	names := make([]string, len(plan.Order))
	for i, u := range plan.Order {
		names[i] = u.Name
	}
	ctlHandler := lifecycle.NewCtlHandler(mgr, names)
	ctlSrv := ctlsock.NewServer(ctlsock.SocketPath, ctlHandler, log)
	go func() {
		if err := ctlSrv.Serve(ctx); err != nil {
			log.Warn("control socket error", "err", err)
		}
	}()

	mgr.StartPlan(ctx, plan.Order)
	log.Info("all units started; waiting for shutdown signal")

	<-ctx.Done()
	log.Info("initiating ordered shutdown")
	mgr.ShutdownPlan(plan.Order)
	log.Info("nura-manager shutdown complete")
	return nil
}
