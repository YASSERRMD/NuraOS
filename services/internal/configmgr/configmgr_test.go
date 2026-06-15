package configmgr_test

import (
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/configmgr"
)

// TestDefaultConfigIsValid verifies DefaultConfig passes schema validation.
func TestDefaultConfigIsValid(t *testing.T) {
	cfg := configmgr.DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig().Validate(): %v", err)
	}
}

// TestValidateRejectsShortContext verifies context_len < 512 is rejected.
func TestValidateRejectsShortContext(t *testing.T) {
	cfg := configmgr.DefaultConfig()
	cfg.Agent.ContextLen = 128
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for context_len=128; got nil")
	}
}

// TestValidateRejectsInvalidPort verifies port < 1024 is rejected.
func TestValidateRejectsInvalidPort(t *testing.T) {
	cfg := configmgr.DefaultConfig()
	cfg.Gateway.Port = 80
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for port=80; got nil")
	}
}

// TestValidateRejectsInvalidFirewallAction verifies bad action is rejected.
func TestValidateRejectsInvalidFirewallAction(t *testing.T) {
	cfg := configmgr.DefaultConfig()
	cfg.Firewall.Rules = []configmgr.FirewallRule{
		{Action: "permit", Proto: "tcp", Port: 8080},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for action='permit'; got nil")
	}
}

// TestValidateAcceptsValidFirewallRules verifies a valid rule set passes.
func TestValidateAcceptsValidFirewallRules(t *testing.T) {
	cfg := configmgr.DefaultConfig()
	cfg.Firewall.Rules = []configmgr.FirewallRule{
		{Action: "allow", Proto: "tcp", Port: 8080},
		{Action: "deny", Proto: "any", Port: 0},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid firewall rules rejected: %v", err)
	}
}

// TestStoreLoadDefaultWhenMissing verifies Load returns DefaultConfig when no
// config file exists.
func TestStoreLoadDefaultWhenMissing(t *testing.T) {
	s := configmgr.NewStore(t.TempDir())
	cfg, err := s.Load()
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if cfg.Gateway.Port != 8080 {
		t.Errorf("default gateway port = %d; want 8080", cfg.Gateway.Port)
	}
}

// TestStoreApplyAndLoad verifies Apply writes and Load reads the config.
func TestStoreApplyAndLoad(t *testing.T) {
	s := configmgr.NewStore(t.TempDir())
	cfg := configmgr.DefaultConfig()
	cfg.Gateway.Port = 9090

	if err := s.Apply(cfg); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Gateway.Port != 9090 {
		t.Errorf("loaded port = %d; want 9090", loaded.Gateway.Port)
	}
	if loaded.Version != 2 {
		t.Errorf("version = %d; want 2 (default is v1, apply increments)", loaded.Version)
	}
}

// TestStoreApplyRejectsInvalidConfig verifies invalid config is not written.
func TestStoreApplyRejectsInvalidConfig(t *testing.T) {
	s := configmgr.NewStore(t.TempDir())
	bad := configmgr.DefaultConfig()
	bad.Gateway.Port = 22 // below 1024

	if err := s.Apply(bad); err == nil {
		t.Error("Apply with invalid port: expected error, got nil")
	}
	// The live config must not have changed.
	cfg, _ := s.Load()
	if cfg.Gateway.Port == 22 {
		t.Error("invalid config was written to disk despite validation failure")
	}
}

// TestDriftDetection verifies DetectDrift reports differences correctly.
func TestDriftDetection(t *testing.T) {
	snap := configmgr.DefaultConfig()
	running := configmgr.DefaultConfig()
	running.Gateway.Port = 9091
	running.Agent.Threads = 8

	report := configmgr.DetectDrift(snap, running)
	if !report.Drifted {
		t.Error("DetectDrift: Drifted = false; want true")
	}

	// Verify the drifted fields are reported.
	fields := make(map[string]bool)
	for _, e := range report.Entries {
		fields[e.Field] = true
	}
	if !fields["gateway.port"] {
		t.Error("gateway.port drift not reported")
	}
	if !fields["agent.threads"] {
		t.Error("agent.threads drift not reported")
	}
}

// TestNoDriftWhenIdentical verifies DetectDrift returns Drifted=false on
// equal configs.
func TestNoDriftWhenIdentical(t *testing.T) {
	cfg := configmgr.DefaultConfig()
	report := configmgr.DetectDrift(cfg, cfg)
	if report.Drifted {
		t.Errorf("DetectDrift on identical configs: Drifted = true; want false")
	}
}

// TestHistory verifies multiple Apply calls are recorded in history.
func TestHistory(t *testing.T) {
	s := configmgr.NewStore(t.TempDir())
	cfg := configmgr.DefaultConfig()

	for i := 0; i < 3; i++ {
		cfg.Gateway.Port = 8080 + i
		if err := s.Apply(cfg); err != nil {
			t.Fatalf("Apply %d: %v", i, err)
		}
	}

	entries, err := s.History()
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(entries) < 3 {
		t.Errorf("History: %d entries; want >= 3", len(entries))
	}
}

// TestRollback verifies RollbackTo restores a previous version.
func TestRollback(t *testing.T) {
	s := configmgr.NewStore(t.TempDir())
	cfg := configmgr.DefaultConfig()
	cfg.Gateway.Port = 8080
	if err := s.Apply(cfg); err != nil {
		t.Fatalf("Apply v1: %v", err)
	}
	savedVersion := 2 // After first Apply from default v1

	cfg.Gateway.Port = 9999
	if err := s.Apply(cfg); err != nil {
		t.Fatalf("Apply v2: %v", err)
	}

	// Rollback to version 2 (port 8080).
	if err := s.RollbackTo(savedVersion); err != nil {
		t.Fatalf("RollbackTo(%d): %v", savedVersion, err)
	}
	loaded, _ := s.Load()
	if loaded.Gateway.Port != 8080 {
		t.Errorf("after rollback port = %d; want 8080", loaded.Gateway.Port)
	}
}
