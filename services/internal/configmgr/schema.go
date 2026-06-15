// Package configmgr provides canonical configuration snapshots, schema
// validation, drift detection, atomic apply-with-validation, and a version
// history with rollback for NuraOS.
//
// The canonical config lives at /data/config/nura.json. Each successful apply
// is appended to the history log at /data/config/history.jsonl. A failed
// validation never touches the live config file.
package configmgr

import (
	"errors"
	"fmt"
	"strings"
)

// AgentConfig holds inference agent settings.
type AgentConfig struct {
	// ModelPath is the path to the GGUF model file.
	ModelPath string `json:"model_path"`
	// ContextLen is the context window in tokens (>= 512).
	ContextLen int `json:"context_len"`
	// Threads is the number of CPU threads for inference (>= 1).
	Threads int `json:"threads"`
}

// GatewayConfig holds HTTP gateway settings.
type GatewayConfig struct {
	// Port is the HTTP port (1024-65535).
	Port int `json:"port"`
	// BindLAN enables LAN binding when true (loopback-only otherwise).
	BindLAN bool `json:"bind_lan"`
	// RateRPS is the per-IP request rate limit (>= 1).
	RateRPS float64 `json:"rate_rps"`
}

// FirewallRule is a single allow or deny rule.
type FirewallRule struct {
	// Action is "allow" or "deny".
	Action string `json:"action"`
	// Proto is "tcp", "udp", or "any".
	Proto string `json:"proto"`
	// Port is the destination port (1-65535) or 0 for any.
	Port int `json:"port"`
	// Src is the source CIDR or empty for any.
	Src string `json:"src,omitempty"`
}

// FirewallConfig holds the ordered list of firewall rules.
type FirewallConfig struct {
	Rules []FirewallRule `json:"rules"`
}

// RoutingConfig holds static route entries.
type RoutingConfig struct {
	// DefaultGateway is the IPv4 default gateway (empty = DHCP).
	DefaultGateway string `json:"default_gateway,omitempty"`
	// StaticRoutes maps destination CIDR to next-hop IP.
	StaticRoutes map[string]string `json:"static_routes,omitempty"`
}

// Config is the canonical NuraOS configuration snapshot.
type Config struct {
	// Version is a monotonically increasing integer; incremented on every apply.
	Version int `json:"version"`
	// Agent holds inference agent parameters.
	Agent AgentConfig `json:"agent"`
	// Gateway holds HTTP gateway parameters.
	Gateway GatewayConfig `json:"gateway"`
	// Firewall holds the ordered rule set.
	Firewall FirewallConfig `json:"firewall"`
	// Routing holds static routing configuration.
	Routing RoutingConfig `json:"routing"`
}

// Validate checks the config for schema violations and returns all errors.
func (c *Config) Validate() error {
	var errs []string

	// Agent
	if c.Agent.ContextLen != 0 && c.Agent.ContextLen < 512 {
		errs = append(errs, fmt.Sprintf("agent.context_len %d is below minimum 512", c.Agent.ContextLen))
	}
	if c.Agent.Threads < 0 {
		errs = append(errs, fmt.Sprintf("agent.threads %d must be >= 0", c.Agent.Threads))
	}

	// Gateway
	if c.Gateway.Port != 0 && (c.Gateway.Port < 1024 || c.Gateway.Port > 65535) {
		errs = append(errs, fmt.Sprintf("gateway.port %d must be in 1024-65535", c.Gateway.Port))
	}
	if c.Gateway.RateRPS < 0 {
		errs = append(errs, fmt.Sprintf("gateway.rate_rps %.2f must be >= 0", c.Gateway.RateRPS))
	}

	// Firewall
	for i, r := range c.Firewall.Rules {
		if r.Action != "allow" && r.Action != "deny" {
			errs = append(errs, fmt.Sprintf("firewall.rules[%d].action %q must be 'allow' or 'deny'", i, r.Action))
		}
		if r.Proto != "tcp" && r.Proto != "udp" && r.Proto != "any" {
			errs = append(errs, fmt.Sprintf("firewall.rules[%d].proto %q must be tcp/udp/any", i, r.Proto))
		}
		if r.Port < 0 || r.Port > 65535 {
			errs = append(errs, fmt.Sprintf("firewall.rules[%d].port %d out of range 0-65535", i, r.Port))
		}
	}

	if len(errs) > 0 {
		return errors.New("config validation failed:\n  " + strings.Join(errs, "\n  "))
	}
	return nil
}

// DefaultConfig returns a minimal valid configuration.
func DefaultConfig() Config {
	return Config{
		Version: 1,
		Agent: AgentConfig{
			ContextLen: 4096,
			Threads:    4,
		},
		Gateway: GatewayConfig{
			Port:    8080,
			BindLAN: false,
			RateRPS: 10,
		},
	}
}
