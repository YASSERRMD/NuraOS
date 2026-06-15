package timesync

import (
	"context"
	"log/slog"
	"time"
)

// Config holds runtime-configurable parameters for the time manager.
type Config struct {
	// NTPServer is an optional SNTP server (hostname or host:port).
	// Empty string disables NTP sync - the system boots correctly offline.
	NTPServer string

	// TZFile is the path to the timezone name file.
	// Defaults to /data/etc/timezone when empty.
	TZFile string

	// RTCDevice is the RTC device path. Defaults to /dev/rtc0 on Linux.
	RTCDevice string
}

// Manager coordinates system time: RTC boot sync, timezone, optional SNTP,
// and clock-step detection. It exposes a monotonic Clock to the rest of the
// system.
type Manager struct {
	log   *slog.Logger
	clock *Clock
	loc   *time.Location
}

// NewManager creates an uninitialised Manager. Call Start to activate it.
func NewManager(log *slog.Logger) *Manager {
	return &Manager{log: log, clock: NewClock(), loc: time.UTC}
}

// Clock returns the Manager's monotonic clock. Safe to use before Start.
func (m *Manager) Clock() *Clock { return m.clock }

// Location returns the configured local timezone (UTC until Start completes).
func (m *Manager) Location() *time.Location { return m.loc }

// Now is a convenience wrapper for Clock().Now().
func (m *Manager) Now() MonoTime { return m.clock.Now() }

// Start performs boot-time time setup and launches background goroutines.
//  1. Load and apply timezone from cfg.TZFile.
//  2. Read the RTC and set the system clock from it.
//  3. If cfg.NTPServer is set, perform an initial SNTP sync.
//  4. Launch goroutines for clock-step logging and periodic NTP refresh.
//
// All failures are logged and handled gracefully; Start always returns nil.
func (m *Manager) Start(ctx context.Context, cfg Config) error {
	// 1. Timezone.
	loc, err := LoadTimezone(cfg.TZFile)
	if err != nil {
		m.log.Warn("timezone load error; using UTC", "err", err)
	}
	m.loc = loc
	ApplyTimezone(loc)
	m.log.Info("timezone applied", "tz", loc.String())

	// 2. RTC.
	rtcT, rtcErr := ReadRTC(cfg.RTCDevice)
	if rtcErr != nil {
		m.log.Warn("RTC unavailable; using system clock", "err", rtcErr)
	} else {
		if sErr := SetSystemTime(rtcT); sErr != nil {
			m.log.Warn("set system time from RTC failed (unprivileged?)", "err", sErr)
		} else {
			m.log.Info("system time set from RTC", "time", rtcT.Format(time.RFC3339))
		}
	}

	// 3. Initial NTP sync.
	if cfg.NTPServer != "" {
		m.syncNTP(cfg.NTPServer)
	}

	// 4. Background tasks.
	go m.runStepLogger(ctx)
	if cfg.NTPServer != "" {
		go m.runNTPRefresh(ctx, cfg.NTPServer)
	}

	return nil
}

func (m *Manager) syncNTP(server string) {
	ntpT, err := SNTPQuery(server)
	if err != nil {
		m.log.Warn("SNTP query failed", "server", server, "err", err)
		return
	}
	if sErr := SetSystemTime(ntpT); sErr != nil {
		m.log.Warn("set system time from NTP failed", "err", sErr)
		return
	}
	m.log.Info("system time synced from NTP",
		"server", server, "time", ntpT.Format(time.RFC3339))
}

// runStepLogger forwards clock step events to the logger.
func (m *Manager) runStepLogger(ctx context.Context) {
	for {
		select {
		case step := <-m.clock.StepEvents():
			m.log.Warn("clock step detected",
				"before", step.Before.Format(time.RFC3339),
				"after", step.After.Format(time.RFC3339),
				"delta", step.Delta.String(),
				"seq", step.Seq)
		case <-ctx.Done():
			return
		}
	}
}

// runNTPRefresh periodically re-syncs from NTP every 30 minutes.
func (m *Manager) runNTPRefresh(ctx context.Context, server string) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.syncNTP(server)
		case <-ctx.Done():
			return
		}
	}
}
