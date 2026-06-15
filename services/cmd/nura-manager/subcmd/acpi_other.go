//go:build !linux

package subcmd

import "log/slog"

// watchACPI is a no-op on non-Linux platforms (SIGPWR is Linux-only).
func watchACPI(trigger func(), log *slog.Logger) {}
