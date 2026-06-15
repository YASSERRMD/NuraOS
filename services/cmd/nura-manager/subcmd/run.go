package subcmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/ctlsock"
	"github.com/yasserrmd/nuraos/services/internal/diskmon"
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

// Shutdown mode values carried on shutdownCh.
const (
	shutdownNormal   = 0 // SIGTERM/SIGINT: ordered stop then exit
	shutdownPoweroff = 1 // ordered stop then syscall.Reboot(POWER_OFF)
	shutdownReboot   = 2 // ordered stop then syscall.Reboot(RESTART)
)

// shutdownTotalTimeout is the maximum wall-clock time allowed for all services
// to stop before the power action is forced. Each service already has a 15s
// per-service grace period; this caps the total across the full shutdown plan.
const shutdownTotalTimeout = 30 * time.Second

// Run loads units from dir, resolves their order, and starts them in sequence
// using the lifecycle Manager. A control socket is exposed for nuractl.
// It blocks until a shutdown trigger arrives (SIGTERM, SIGINT, SIGPWR, or a
// nuractl poweroff/reboot command), then performs ordered shutdown and -- for
// poweroff/reboot -- calls the appropriate kernel reboot syscall.
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
	defer cancel()

	// shutdownCh carries exactly one shutdown mode. Buffered so senders never block.
	shutdownCh := make(chan int, 1)
	deliverShutdown := func(mode int) {
		select {
		case shutdownCh <- mode:
		default: // already triggered; first one wins
		}
	}

	// SIGTERM / SIGINT: ordered stop then exit normally.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigs
		log.Info("received signal", "signal", sig)
		deliverShutdown(shutdownNormal)
	}()

	// ACPI power-button event (SIGPWR on Linux): ordered stop then poweroff.
	watchACPI(func() { deliverShutdown(shutdownPoweroff) }, log)

	// System identity: stable machine-id and hostname (set before starting services).
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

	// Disk space monitor: automatic reclaim at warn, log at critical.
	diskMon := &diskmon.Monitor{
		Path: dataDir,
		Log:  log,
		OnWarn: func(u diskmon.Usage) {
			freed, err := diskmon.Reclaim(diskmon.ReclaimOptions{
				DataDir:    dataDir,
				SessionCap: 512 * 1024 * 1024,
				LogsCap:    128 * 1024 * 1024,
			})
			if err != nil {
				log.Warn("disk warn: auto-reclaim error", "err", err)
			} else {
				log.Info("disk warn: auto-reclaim complete", "freed_bytes", freed, "used_pct", u.UsedPct)
			}
		},
		OnCritical: func(u diskmon.Usage) {
			log.Error("disk critical: new sessions will be refused", "used_pct", u.UsedPct)
		},
	}
	go diskMon.Run(ctx)

	mgr := lifecycle.NewManager(log, jw)

	// Start the control socket server so nuractl can query and control services.
	names := make([]string, len(plan.Order))
	for i, u := range plan.Order {
		names[i] = u.Name
	}
	ctlHandler := lifecycle.NewCtlHandler(mgr, names, func(reboot bool) {
		if reboot {
			log.Info("reboot requested via nuractl")
			deliverShutdown(shutdownReboot)
		} else {
			log.Info("poweroff requested via nuractl")
			deliverShutdown(shutdownPoweroff)
		}
	})
	ctlSrv := ctlsock.NewServer(ctlsock.SocketPath, ctlHandler, log)
	go func() {
		if err := ctlSrv.Serve(ctx); err != nil {
			log.Warn("control socket error", "err", err)
		}
	}()

	mgr.StartPlan(ctx, plan.Order)
	log.Info("all units started; waiting for shutdown signal")

	// Block until a shutdown trigger arrives.
	mode := <-shutdownCh
	cancel() // stop all service contexts

	log.Info("initiating ordered shutdown")
	shutdownDone := make(chan struct{})
	go func() {
		mgr.ShutdownPlan(plan.Order)
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		log.Info("ordered shutdown complete")
	case <-time.After(shutdownTotalTimeout):
		log.Warn("shutdown timeout exceeded; forcing power action",
			"timeout", shutdownTotalTimeout)
	}

	// Flush journal before any power state transition. Close() is idempotent
	// so the deferred close above is a safe no-op after this point.
	if jw != nil {
		_ = jw.Close()
	}

	switch mode {
	case shutdownPoweroff:
		log.Info("halting system")
		if err := lifecycle.SysHalt(); err != nil {
			log.Error("poweroff syscall failed", "err", err)
		}
	case shutdownReboot:
		log.Info("rebooting system")
		if err := lifecycle.SysReboot(); err != nil {
			log.Error("reboot syscall failed", "err", err)
		}
	}

	log.Info("nura-manager shutdown complete")
	return nil
}
