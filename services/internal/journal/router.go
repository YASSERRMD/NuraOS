package journal

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
)

// MultiHandler is a slog.Handler that fans out records to multiple handlers.
// Each handler's Enabled gate is respected before delivery.
type MultiHandler struct {
	mu       sync.RWMutex
	handlers []slog.Handler
}

// NewMultiHandler returns a MultiHandler that fans out to the given handlers.
func NewMultiHandler(hs ...slog.Handler) *MultiHandler {
	return &MultiHandler{handlers: hs}
}

func (m *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	m.mu.RLock()
	hs := m.handlers
	m.mu.RUnlock()
	for _, h := range hs {
		if h.Enabled(ctx, r.Level) {
			_ = h.Handle(ctx, r)
		}
	}
	return nil
}

func (m *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	m.mu.RLock()
	defer m.mu.RUnlock()
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithAttrs(attrs)
	}
	return &MultiHandler{handlers: handlers}
}

func (m *MultiHandler) WithGroup(name string) slog.Handler {
	m.mu.RLock()
	defer m.mu.RUnlock()
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithGroup(name)
	}
	return &MultiHandler{handlers: handlers}
}

// SlogJournalHandler is a slog.Handler that converts slog.Record to
// journal.Record and writes it to a *Writer. It captures all severity levels.
type SlogJournalHandler struct {
	w       *Writer
	service string
}

// NewSlogJournalHandler creates a handler that tags records with service.
func NewSlogJournalHandler(jw *Writer, service string) *SlogJournalHandler {
	return &SlogJournalHandler{w: jw, service: service}
}

func (h *SlogJournalHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *SlogJournalHandler) Handle(_ context.Context, r slog.Record) error {
	return h.w.Write(Record{
		Time:    r.Time,
		Service: h.service,
		PID:     os.Getpid(),
		Pri:     slogLevelToPri(r.Level),
		Message: r.Message,
	})
}

func (h *SlogJournalHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	h2 := *h
	return &h2
}

func (h *SlogJournalHandler) WithGroup(name string) slog.Handler {
	h2 := *h
	h2.service = h.service + "/" + name
	return &h2
}

// NewRouter returns a slog.Handler implementing severity-based routing:
//   - all records are written to jw (if non-nil)
//   - Info-and-above records are written to console
//
// On NuraOS the console IS the serial port (ttyS0), which is the operator's
// only live view of a headless boot, so Info-level lifecycle logs
// ("nura-manager starting", "all units started", ...) must reach it -- not just
// the on-disk journal. (The integration suites assert that serial carries
// INFO-level structured log lines.) The journal still captures the full stream.
//
// service is used as the journal record's svc field for manager-internal logs.
// When jw is nil, the full log stream is written to console at Info level.
func NewRouter(jw *Writer, console io.Writer, service string) slog.Handler {
	consoleHandler := slog.NewTextHandler(console, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	if jw == nil {
		return slog.NewTextHandler(console, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	}
	return NewMultiHandler(
		NewSlogJournalHandler(jw, service),
		consoleHandler,
	)
}

// slogLevelToPri maps slog severity levels to RFC 5424 journal priorities.
func slogLevelToPri(level slog.Level) Priority {
	switch {
	case level >= slog.LevelError:
		return PriError
	case level >= slog.LevelWarn:
		return PriWarning
	case level >= slog.LevelInfo:
		return PriInfo
	default:
		return PriDebug
	}
}
