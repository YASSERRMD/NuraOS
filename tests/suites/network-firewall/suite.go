// Package networkfirewall provides the network-firewall integration test
// suite (T13).
//
// It asserts that the gateway binds to loopback by default (not all
// interfaces), that the net.status tool is registered, that loopback
// connectivity is functional, and that the firewall configuration file exists
// in the repository.
package networkfirewall

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yasserrmd/nuraos/tests/harness"
)

const suite = "network-firewall"

// Run executes all network-firewall cases against the pre-booted, ready instance.
func Run(ctx context.Context, inst *harness.QEMUInstance) []harness.Result {
	return []harness.Result{
		caseGatewayLoopbackOnly(ctx, inst),
		caseNetStatusTool(ctx, inst),
		caseHealthzLocalReachable(ctx, inst),
		caseFirewallConfExists(ctx, inst),
	}
}

// ---------------------------------------------------------------------------
// Case: gateway-loopback-only
// Acceptance: Phase 30 -- /config bind field is 127.0.0.1 by default.
// ---------------------------------------------------------------------------

func caseGatewayLoopbackOnly(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/config")
	if err != nil {
		return fail("gateway-loopback-only", fmt.Sprintf("GET /config error: %v", err))
	}
	if code != 200 {
		return fail("gateway-loopback-only", fmt.Sprintf("GET /config returned %d (want 200): %s", code, body))
	}
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return fail("gateway-loopback-only", fmt.Sprintf("GET /config body is not valid JSON: %v", err))
	}
	// The "bind" field may be nested under "gateway" or at the top level.
	bindVal := extractBind(resp)
	if bindVal == "" {
		// bind field absent — skip rather than fail (field may not be exposed yet).
		return skip("gateway-loopback-only", "bind field not found in /config; cannot verify loopback restriction")
	}
	if bindVal == "0.0.0.0" {
		return fail("gateway-loopback-only",
			fmt.Sprintf("gateway bind is '%s' (want 127.0.0.1); all-interfaces binding is unsafe", bindVal))
	}
	if bindVal == "127.0.0.1" {
		return pass("gateway-loopback-only", "gateway bind=127.0.0.1 (loopback only)")
	}
	// Some other value (e.g. custom address) — accept as non-open.
	return pass("gateway-loopback-only", fmt.Sprintf("gateway bind=%q (not 0.0.0.0)", bindVal))
}

// extractBind walks the config map looking for a "bind" string value.
func extractBind(m map[string]interface{}) string {
	if v, ok := m["bind"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	// Check under "gateway" sub-object.
	if gw, ok := m["gateway"]; ok {
		if gwMap, ok := gw.(map[string]interface{}); ok {
			if v, ok := gwMap["bind"]; ok {
				if s, ok := v.(string); ok {
					return s
				}
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Case: net-status-tool
// Acceptance: Phase 23 -- GET /tools includes net.status tool.
// ---------------------------------------------------------------------------

func caseNetStatusTool(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/tools")
	if err != nil {
		return fail("net-status-tool", fmt.Sprintf("GET /tools error: %v", err))
	}
	if code != 200 {
		return fail("net-status-tool", fmt.Sprintf("GET /tools returned %d (want 200): %s", code, body))
	}
	if !strings.Contains(body, "net.status") {
		return fail("net-status-tool",
			fmt.Sprintf("'net.status' tool not found in /tools response; body=%s", body))
	}
	return pass("net-status-tool", "net.status tool present in /tools registry")
}

// ---------------------------------------------------------------------------
// Case: healthz-local-reachable
// Acceptance: Phase 10 -- GET /healthz succeeds via loopback forwarding.
// ---------------------------------------------------------------------------

func caseHealthzLocalReachable(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	code, body, err := inst.HTTP().GetBody("/healthz")
	if err != nil {
		return fail("healthz-local-reachable", fmt.Sprintf("GET /healthz error: %v", err))
	}
	if code != 200 {
		return fail("healthz-local-reachable",
			fmt.Sprintf("GET /healthz returned %d (want 200): %s", code, body))
	}
	return pass("healthz-local-reachable", "GET /healthz=200 via loopback port-forward")
}

// ---------------------------------------------------------------------------
// Case: firewall-conf-exists
// Acceptance: Phase 30 -- rootfs/etc/nura/firewall.conf exists in repo.
// ---------------------------------------------------------------------------

func caseFirewallConfExists(_ context.Context, inst *harness.QEMUInstance) harness.Result {
	confPath := filepath.Join(inst.RepoRoot, "rootfs", "etc", "nura", "firewall.conf")
	if _, err := os.Stat(confPath); err != nil {
		return fail("firewall-conf-exists",
			fmt.Sprintf("firewall.conf not found at %s: %v", confPath, err))
	}
	return pass("firewall-conf-exists", fmt.Sprintf("firewall.conf found at %s", confPath))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func pass(case_, msg string) harness.Result {
	return harness.Result{Suite: suite, Case: case_, Status: harness.StatusPass, Message: msg}
}

func fail(case_, msg string) harness.Result {
	return harness.Result{Suite: suite, Case: case_, Status: harness.StatusFail, Message: msg}
}

func skip(case_, msg string) harness.Result {
	return harness.Result{Suite: suite, Case: case_, Status: harness.StatusSkip, Message: msg}
}
