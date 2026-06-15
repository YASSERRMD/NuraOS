package sysmetrics_test

import (
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/sysmetrics"
)

// TestCollectDoesNotPanic verifies Collect returns a result without panicking.
func TestCollectDoesNotPanic(t *testing.T) {
	c := sysmetrics.NewCollector([]string{"nura-agent", "gateway", "llama-server"})
	s := c.Collect()
	// We don't assert specific values because the test environment may not have
	// cgroups or /proc/net/dev. Just verify the call doesn't panic.
	_ = s.EntropyAvailBits
	_ = s.Interfaces
	_ = s.CgroupStats
}

// TestNilCollectorIsSafe verifies the nil receiver does not panic.
func TestNilCollectorIsSafe(t *testing.T) {
	var c *sysmetrics.Collector
	s := c.Collect()
	if s.EntropyAvailBits != 0 {
		t.Errorf("nil collector: EntropyAvailBits = %d; want 0", s.EntropyAvailBits)
	}
	if len(s.Interfaces) != 0 {
		t.Errorf("nil collector: Interfaces = %v; want empty", s.Interfaces)
	}
}

// TestNewCollectorRetainsServices verifies the services list is stored.
func TestNewCollectorRetainsServices(t *testing.T) {
	svcs := []string{"gateway", "llama-server"}
	c := sysmetrics.NewCollector(svcs)
	if len(c.CgroupServices) != 2 {
		t.Errorf("CgroupServices len = %d; want 2", len(c.CgroupServices))
	}
}
