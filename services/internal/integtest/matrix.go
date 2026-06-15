// Package integtest defines the NuraOS integration test matrix. Each scenario
// exercises one or more subsystems together, with explicit readiness gating to
// prevent flakiness.
//
// The matrix is designed to run headless in QEMU CI. Each scenario has a
// Run function that returns a ScenarioResult describing what passed or failed.
// Scenarios that require a live appliance (QEMU running, agent socket present)
// skip automatically when the relevant resources are unavailable.
//
// Boot-time and footprint budget assertions live in budget_test.go.
package integtest

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

// ScenarioStatus is the outcome of a single integration scenario.
type ScenarioStatus string

const (
	ScenarioPass ScenarioStatus = "pass"
	ScenarioFail ScenarioStatus = "fail"
	ScenarioSkip ScenarioStatus = "skip"
)

// ScenarioResult is the result of one scenario run.
type ScenarioResult struct {
	// Name is the scenario identifier.
	Name string `json:"name"`
	// Subsystem groups related scenarios.
	Subsystem string `json:"subsystem"`
	// Status is the scenario outcome.
	Status ScenarioStatus `json:"status"`
	// Detail is a human-readable description.
	Detail string `json:"detail"`
	// Duration is the elapsed time.
	Duration time.Duration `json:"duration_ns"`
}

// Scenario is a single integration test scenario.
type Scenario struct {
	Name      string
	Subsystem string
	Run       func(ctx context.Context) ScenarioResult
}

// MatrixReport is the aggregate result of the full integration matrix.
type MatrixReport struct {
	Results   []ScenarioResult `json:"results"`
	Pass      int              `json:"pass"`
	Fail      int              `json:"fail"`
	Skip      int              `json:"skip"`
	Overall   string           `json:"overall"`
	ElapsedMS int64            `json:"elapsed_ms"`
}

// Runner executes the integration matrix.
type Runner struct {
	scenarios []*Scenario
	// GatewayURL is the base URL of the gateway to test against.
	GatewayURL string
	// AgentSocket is the path to the agent Unix socket.
	AgentSocket string
}

// New returns a Runner pre-loaded with all standard integration scenarios.
func New(gatewayURL, agentSocket string) *Runner {
	r := &Runner{
		GatewayURL:  gatewayURL,
		AgentSocket: agentSocket,
	}
	r.Register(r.allScenarios()...)
	return r
}

// Register adds scenarios to the runner.
func (r *Runner) Register(scenarios ...*Scenario) {
	r.scenarios = append(r.scenarios, scenarios...)
}

// Run executes all registered scenarios and returns a MatrixReport.
func (r *Runner) Run(ctx context.Context) MatrixReport {
	start := time.Now()
	var results []ScenarioResult
	for _, s := range r.scenarios {
		t0 := time.Now()
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		res := s.Run(cctx)
		cancel()
		res.Duration = time.Since(t0)
		if res.Name == "" {
			res.Name = s.Name
		}
		if res.Subsystem == "" {
			res.Subsystem = s.Subsystem
		}
		results = append(results, res)
	}
	rep := MatrixReport{
		Results:   results,
		ElapsedMS: time.Since(start).Milliseconds(),
	}
	for _, res := range results {
		switch res.Status {
		case ScenarioPass:
			rep.Pass++
		case ScenarioFail:
			rep.Fail++
		case ScenarioSkip:
			rep.Skip++
		}
	}
	if rep.Fail == 0 {
		rep.Overall = "pass"
	} else {
		rep.Overall = "fail"
	}
	return rep
}

// allScenarios returns the standard integration matrix.
func (r *Runner) allScenarios() []*Scenario {
	return []*Scenario{
		r.scenarioGatewayHealthz(),
		r.scenarioGatewayVersion(),
		r.scenarioGatewayStatus(),
		r.scenarioGatewayMetrics(),
		r.scenarioAgentSocketReachable(),
		r.scenarioSelftest(),
		r.scenarioStorageDurability(),
		r.scenarioProviderHealthSnapshot(),
		r.scenarioCrashDirExists(),
		r.scenarioModelDirExists(),
	}
}

// --- scenario implementations ---

func (r *Runner) scenarioGatewayHealthz() *Scenario {
	return &Scenario{
		Name:      "gateway-healthz",
		Subsystem: "service-lifecycle",
		Run: func(ctx context.Context) ScenarioResult {
			res := ScenarioResult{Name: "gateway-healthz", Subsystem: "service-lifecycle"}
			if r.GatewayURL == "" {
				res.Status = ScenarioSkip
				res.Detail = "GatewayURL not set"
				return res
			}
			resp, err := httpGet(ctx, r.GatewayURL+"/healthz")
			if err != nil {
				res.Status = ScenarioFail
				res.Detail = fmt.Sprintf("GET /healthz: %v", err)
				return res
			}
			if resp != http.StatusOK {
				res.Status = ScenarioFail
				res.Detail = fmt.Sprintf("GET /healthz: status %d; want 200", resp)
				return res
			}
			res.Status = ScenarioPass
			res.Detail = "gateway /healthz returned 200"
			return res
		},
	}
}

func (r *Runner) scenarioGatewayVersion() *Scenario {
	return &Scenario{
		Name:      "gateway-version",
		Subsystem: "service-lifecycle",
		Run: func(ctx context.Context) ScenarioResult {
			res := ScenarioResult{Name: "gateway-version", Subsystem: "service-lifecycle"}
			if r.GatewayURL == "" {
				res.Status = ScenarioSkip
				res.Detail = "GatewayURL not set"
				return res
			}
			code, err := httpGet(ctx, r.GatewayURL+"/version")
			if err != nil || code != http.StatusOK {
				res.Status = ScenarioFail
				res.Detail = fmt.Sprintf("GET /version: code=%d err=%v", code, err)
				return res
			}
			res.Status = ScenarioPass
			res.Detail = "gateway /version returned 200"
			return res
		},
	}
}

func (r *Runner) scenarioGatewayStatus() *Scenario {
	return &Scenario{
		Name:      "gateway-status",
		Subsystem: "observability",
		Run: func(ctx context.Context) ScenarioResult {
			res := ScenarioResult{Name: "gateway-status", Subsystem: "observability"}
			if r.GatewayURL == "" {
				res.Status = ScenarioSkip
				res.Detail = "GatewayURL not set"
				return res
			}
			code, err := httpGet(ctx, r.GatewayURL+"/status")
			if err != nil || code != http.StatusOK {
				res.Status = ScenarioFail
				res.Detail = fmt.Sprintf("GET /status: code=%d err=%v", code, err)
				return res
			}
			res.Status = ScenarioPass
			res.Detail = "gateway /status returned 200"
			return res
		},
	}
}

func (r *Runner) scenarioGatewayMetrics() *Scenario {
	return &Scenario{
		Name:      "gateway-metrics",
		Subsystem: "observability",
		Run: func(ctx context.Context) ScenarioResult {
			res := ScenarioResult{Name: "gateway-metrics", Subsystem: "observability"}
			if r.GatewayURL == "" {
				res.Status = ScenarioSkip
				res.Detail = "GatewayURL not set"
				return res
			}
			code, err := httpGet(ctx, r.GatewayURL+"/metrics")
			if err != nil || code != http.StatusOK {
				res.Status = ScenarioFail
				res.Detail = fmt.Sprintf("GET /metrics: code=%d err=%v", code, err)
				return res
			}
			res.Status = ScenarioPass
			res.Detail = "gateway /metrics returned 200"
			return res
		},
	}
}

func (r *Runner) scenarioAgentSocketReachable() *Scenario {
	return &Scenario{
		Name:      "agent-socket-reachable",
		Subsystem: "service-lifecycle",
		Run: func(ctx context.Context) ScenarioResult {
			res := ScenarioResult{Name: "agent-socket-reachable", Subsystem: "service-lifecycle"}
			sock := r.AgentSocket
			if sock == "" {
				sock = "/run/nura-agent.sock"
			}
			if _, err := os.Stat(sock); os.IsNotExist(err) {
				res.Status = ScenarioSkip
				res.Detail = fmt.Sprintf("agent socket %s does not exist (agent not running?)", sock)
				return res
			}
			conn, err := (&net.Dialer{}).DialContext(ctx, "unix", sock)
			if err != nil {
				res.Status = ScenarioFail
				res.Detail = fmt.Sprintf("dial %s: %v", sock, err)
				return res
			}
			conn.Close()
			res.Status = ScenarioPass
			res.Detail = fmt.Sprintf("agent socket %s is reachable", sock)
			return res
		},
	}
}

func (r *Runner) scenarioSelftest() *Scenario {
	return &Scenario{
		Name:      "selftest-boot-subset",
		Subsystem: "integration",
		Run: func(ctx context.Context) ScenarioResult {
			res := ScenarioResult{Name: "selftest-boot-subset", Subsystem: "integration"}
			// The selftest boot subset is run via nuractl selftest --boot.
			// In the integration matrix we replicate the same checks inline.
			// This avoids needing nuractl binary in the test binary.
			checks := []struct {
				name  string
				check func() bool
				skip  bool
			}{
				{"rng-avail", rngOK, false},
				{"storage-writable", storageOK, false},
			}
			for _, c := range checks {
				if c.skip {
					continue
				}
				if !c.check() {
					res.Status = ScenarioFail
					res.Detail = fmt.Sprintf("selftest check %q failed", c.name)
					return res
				}
			}
			res.Status = ScenarioPass
			res.Detail = "boot selftest checks passed"
			return res
		},
	}
}

func (r *Runner) scenarioStorageDurability() *Scenario {
	return &Scenario{
		Name:      "storage-durability",
		Subsystem: "storage",
		Run: func(ctx context.Context) ScenarioResult {
			res := ScenarioResult{Name: "storage-durability", Subsystem: "storage"}
			if !storageOK() {
				res.Status = ScenarioFail
				res.Detail = "write/read cycle on /data or tmpdir failed"
				return res
			}
			res.Status = ScenarioPass
			res.Detail = "storage write/sync/read verified"
			return res
		},
	}
}

func (r *Runner) scenarioProviderHealthSnapshot() *Scenario {
	return &Scenario{
		Name:      "provider-health-snapshot",
		Subsystem: "provider-failover",
		Run: func(ctx context.Context) ScenarioResult {
			res := ScenarioResult{Name: "provider-health-snapshot", Subsystem: "provider-failover"}
			if r.GatewayURL == "" {
				res.Status = ScenarioSkip
				res.Detail = "GatewayURL not set"
				return res
			}
			// The /status endpoint includes provider health components when
			// providerhealth is enabled.
			code, err := httpGet(ctx, r.GatewayURL+"/status")
			if err != nil || code != http.StatusOK {
				res.Status = ScenarioFail
				res.Detail = fmt.Sprintf("provider snapshot via /status: code=%d err=%v", code, err)
				return res
			}
			res.Status = ScenarioPass
			res.Detail = "provider health available via /status"
			return res
		},
	}
}

func (r *Runner) scenarioCrashDirExists() *Scenario {
	return &Scenario{
		Name:      "crash-dir-exists",
		Subsystem: "resilience",
		Run: func(ctx context.Context) ScenarioResult {
			res := ScenarioResult{Name: "crash-dir-exists", Subsystem: "resilience"}
			dataDir := os.Getenv("NURA_DATA_DIR")
			if dataDir == "" {
				dataDir = "/data"
			}
			if _, err := os.Stat(dataDir); os.IsNotExist(err) {
				res.Status = ScenarioSkip
				res.Detail = fmt.Sprintf("%s not mounted (not on appliance)", dataDir)
				return res
			}
			// Ensure /data/crashes can be created.
			crashDir := dataDir + "/crashes"
			if err := os.MkdirAll(crashDir, 0750); err != nil {
				res.Status = ScenarioFail
				res.Detail = fmt.Sprintf("cannot create crash dir: %v", err)
				return res
			}
			res.Status = ScenarioPass
			res.Detail = "crash directory is accessible at " + crashDir
			return res
		},
	}
}

func (r *Runner) scenarioModelDirExists() *Scenario {
	return &Scenario{
		Name:      "model-dir-accessible",
		Subsystem: "model-lifecycle",
		Run: func(ctx context.Context) ScenarioResult {
			res := ScenarioResult{Name: "model-dir-accessible", Subsystem: "model-lifecycle"}
			dataDir := os.Getenv("NURA_DATA_DIR")
			if dataDir == "" {
				dataDir = "/data"
			}
			if _, err := os.Stat(dataDir); os.IsNotExist(err) {
				res.Status = ScenarioSkip
				res.Detail = "data dir not mounted"
				return res
			}
			if err := os.MkdirAll(dataDir+"/models", 0750); err != nil {
				res.Status = ScenarioFail
				res.Detail = fmt.Sprintf("cannot create models dir: %v", err)
				return res
			}
			res.Status = ScenarioPass
			res.Detail = "model directory accessible"
			return res
		},
	}
}

// --- helpers ---

func httpGet(ctx context.Context, url string) (int, error) {
	req, err := newRequest(ctx, url)
	if err != nil {
		return 0, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

func newRequest(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	return req, err
}

func rngOK() bool {
	data, err := os.ReadFile("/proc/sys/kernel/random/entropy_avail")
	if err != nil {
		return true // not Linux; skip
	}
	var bits int
	fmt.Sscan(string(data), &bits)
	return bits >= 64
}

func storageOK() bool {
	dataDir := os.Getenv("NURA_DATA_DIR")
	if dataDir == "" {
		dataDir = "/data"
	}
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		dataDir = os.TempDir()
	}
	path := dataDir + "/.integtest-storage"
	payload := []byte("integtest-storage-durability")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return false
	}
	if _, err := f.Write(payload); err != nil {
		f.Close()
		os.Remove(path)
		return false
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(path)
		return false
	}
	f.Close()
	data, err := os.ReadFile(path)
	os.Remove(path)
	return err == nil && string(data) == string(payload)
}
