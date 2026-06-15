//go:build linux

package subcmd

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// watchACPI registers a SIGPWR handler (sent by the kernel on ACPI power-button
// events) and calls trigger() when the signal arrives. trigger must be
// non-blocking; it is called from a dedicated goroutine.
func watchACPI(trigger func(), log *slog.Logger) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGPWR)
	go func() {
		<-ch
		log.Info("ACPI power button event; initiating poweroff")
		trigger()
	}()
}
