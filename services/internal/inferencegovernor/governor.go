// Package inferencegovernor manages cgroup resource limits for the llama-server
// inference process and enforces a memory guard that refuses to load a model
// that would exceed the cgroup memory budget.
//
// Priority policy:
//   - gateway:     cpu.weight = 400 (highest -- serves interactive user requests)
//   - nura-agent:  cpu.weight = 200 (medium -- coordinates responses)
//   - llama-server:cpu.weight = 100 (lowest -- background inference work)
//
// This ensures that interactive control-plane operations (health checks, model
// management, API requests) are never starved by ongoing inference.
//
// The Governor also polls the inference cgroup periodically and publishes
// resource events on the event bus so operators can react to memory pressure
// before an OOM kill occurs.
package inferencegovernor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/yasserrmd/nuraos/services/internal/cgroup"
	"github.com/yasserrmd/nuraos/services/internal/eventbus"
)

const (
	// InferenceService is the cgroup service name for llama-server.
	InferenceService = "llama-server"

	// MemoryHighWatermark is the fraction of the cgroup memory limit at which a
	// TypeInferenceMemoryHigh event is published (90%).
	MemoryHighWatermark = 0.90

	// DefaultPollInterval is how often the governor polls the cgroup.
	DefaultPollInterval = 15 * time.Second
)

// ModelSpec describes a model that the caller wants to load.
type ModelSpec struct {
	Name    string // human-readable name for logging
	RAMBytes uint64 // estimated peak RAM in bytes (from model manifest or file size)
}

// Governor watches the inference cgroup and enforces the memory guard.
type Governor struct {
	mgr      *cgroup.Manager
	bus      *eventbus.Bus
	log      *slog.Logger
	interval time.Duration
}

// New creates a Governor using the default cgroup manager and poll interval.
func New(bus *eventbus.Bus, log *slog.Logger) *Governor {
	return &Governor{
		mgr:      cgroup.NewManager(),
		bus:      bus,
		log:      log,
		interval: DefaultPollInterval,
	}
}

// CheckModelFits returns nil when the model can be loaded without exceeding
// the inference cgroup memory limit. It returns an error if the model's
// estimated RAM usage would push current usage over the configured limit.
//
// When no cgroup limit is set (memory.max == "max"), this check always passes.
func (g *Governor) CheckModelFits(spec ModelSpec) error {
	stats := g.mgr.ReadStats(InferenceService)
	if stats == nil || stats.MemoryMax == 0 {
		// No limit configured or cgroup not available; allow load.
		return nil
	}

	projected := stats.MemoryCurrent + spec.RAMBytes
	if projected > stats.MemoryMax {
		err := fmt.Errorf(
			"model %q (%.0f MiB) would push inference memory to %.0f MiB, "+
				"exceeding cgroup limit %.0f MiB (current: %.0f MiB)",
			spec.Name,
			mib(spec.RAMBytes),
			mib(projected),
			mib(stats.MemoryMax),
			mib(stats.MemoryCurrent),
		)
		if g.bus != nil {
			g.bus.Publish(eventbus.NewEvent(eventbus.TypeInferenceModelRefused, "inferencegovernor", map[string]any{
				"model":          spec.Name,
				"model_ram_bytes": spec.RAMBytes,
				"current_bytes":  stats.MemoryCurrent,
				"limit_bytes":    stats.MemoryMax,
			}))
		}
		return err
	}
	return nil
}

// Run polls the inference cgroup on g.interval and publishes events on the bus.
// It exits when ctx is cancelled. Safe to call in a goroutine.
func (g *Governor) Run(ctx context.Context) {
	ticker := time.NewTicker(g.interval)
	defer ticker.Stop()

	var lastOOM uint64

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.poll(ctx, &lastOOM)
		}
	}
}

func (g *Governor) poll(_ context.Context, lastOOM *uint64) {
	stats := g.mgr.ReadStats(InferenceService)
	if stats == nil {
		return
	}

	// Publish periodic stats snapshot.
	if g.bus != nil {
		g.bus.Publish(eventbus.NewEvent(eventbus.TypeInferenceCPUStats, "inferencegovernor", map[string]any{
			"service":            InferenceService,
			"cpu_usage_sec":      float64(stats.CPUUsageUsec) / 1e6,
			"memory_current_mib": mib(stats.MemoryCurrent),
			"memory_max_mib":     mib(stats.MemoryMax),
			"oom_kills":          stats.OOMKills,
		}))
	}

	// Detect new OOM kills.
	if stats.OOMKills > *lastOOM {
		delta := stats.OOMKills - *lastOOM
		*lastOOM = stats.OOMKills
		g.log.Error("OOM kill detected in inference cgroup",
			"service", InferenceService,
			"new_oom_kills", delta,
			"total", stats.OOMKills)
		if g.bus != nil {
			g.bus.Publish(eventbus.NewEvent(eventbus.TypeInferenceMemoryOOM, "inferencegovernor", map[string]any{
				"service":    InferenceService,
				"oom_kills":  stats.OOMKills,
				"new_kills":  delta,
			}))
		}
	}

	// Warn on high memory watermark.
	if stats.MemoryMax > 0 {
		ratio := float64(stats.MemoryCurrent) / float64(stats.MemoryMax)
		if ratio >= MemoryHighWatermark {
			g.log.Warn("inference cgroup memory pressure",
				"service", InferenceService,
				"used_mib", mib(stats.MemoryCurrent),
				"limit_mib", mib(stats.MemoryMax),
				"pct", fmt.Sprintf("%.0f%%", ratio*100))
			if g.bus != nil {
				g.bus.Publish(eventbus.NewEvent(eventbus.TypeInferenceMemoryHigh, "inferencegovernor", map[string]any{
					"service":      InferenceService,
					"used_bytes":   stats.MemoryCurrent,
					"limit_bytes":  stats.MemoryMax,
					"used_pct":     ratio * 100,
				}))
			}
		}
	}
}

func mib(b uint64) float64 { return float64(b) / (1024 * 1024) }
