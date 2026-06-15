package subcmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/yasserrmd/nuraos/services/internal/lifecycle"
	"github.com/yasserrmd/nuraos/services/internal/resolver"
	"github.com/yasserrmd/nuraos/services/internal/unit"
)

// Run loads units from dir, resolves their order, and starts them in sequence
// using the lifecycle Manager. It blocks until SIGTERM/SIGINT, then performs
// ordered shutdown.
func Run(dir string) error {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

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

	mgr := lifecycle.NewManager(log)
	mgr.StartPlan(ctx, plan.Order)
	log.Info("all units started; waiting for shutdown signal")

	<-ctx.Done()
	log.Info("initiating ordered shutdown")
	mgr.ShutdownPlan(plan.Order)
	log.Info("nura-manager shutdown complete")
	return nil
}
