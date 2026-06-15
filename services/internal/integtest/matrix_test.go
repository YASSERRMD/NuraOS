package integtest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/integtest"
)

func TestRunnerSkipsWhenNoGateway(t *testing.T) {
	r := integtest.New("", "")
	rep := r.Run(context.Background())
	// Without a gateway URL, gateway scenarios must skip and not fail.
	for _, res := range rep.Results {
		if isGatewayScenario(res.Name) && res.Status == integtest.ScenarioFail {
			t.Errorf("scenario %q: expected skip without gateway; got fail: %s", res.Name, res.Detail)
		}
	}
}

func TestRunnerPassesStorageDurability(t *testing.T) {
	r := integtest.New("", "")
	rep := r.Run(context.Background())
	for _, res := range rep.Results {
		if res.Name == "storage-durability" {
			if res.Status == integtest.ScenarioFail {
				t.Errorf("storage-durability: %s", res.Detail)
			}
			return
		}
	}
	t.Error("storage-durability scenario not found")
}

func TestRunnerPassesSelftest(t *testing.T) {
	r := integtest.New("", "")
	rep := r.Run(context.Background())
	for _, res := range rep.Results {
		if res.Name == "selftest-boot-subset" {
			if res.Status == integtest.ScenarioFail {
				t.Errorf("selftest-boot-subset: %s", res.Detail)
			}
			return
		}
	}
	t.Error("selftest-boot-subset scenario not found")
}

func TestRunnerWithFakeGatewayHealthy(t *testing.T) {
	srv := fakeGateway(http.StatusOK)
	defer srv.Close()

	r := integtest.New(srv.URL, "")
	rep := r.Run(context.Background())

	for _, res := range rep.Results {
		if isGatewayScenario(res.Name) && res.Status == integtest.ScenarioFail {
			t.Errorf("scenario %q: fail against healthy gateway: %s", res.Name, res.Detail)
		}
	}
}

func TestRunnerWithFakeGatewayUnhealthy(t *testing.T) {
	srv := fakeGateway(http.StatusServiceUnavailable)
	defer srv.Close()

	r := integtest.New(srv.URL, "")
	rep := r.Run(context.Background())

	gatewayFails := 0
	for _, res := range rep.Results {
		if isGatewayScenario(res.Name) && res.Status == integtest.ScenarioFail {
			gatewayFails++
		}
	}
	if gatewayFails == 0 {
		t.Error("expected at least one gateway scenario to fail with 503 gateway")
	}
}

func TestRegisterCustomScenario(t *testing.T) {
	r := integtest.New("", "")
	called := false
	r.Register(&integtest.Scenario{
		Name:      "custom-scenario",
		Subsystem: "test",
		Run: func(ctx context.Context) integtest.ScenarioResult {
			called = true
			return integtest.ScenarioResult{
				Name:      "custom-scenario",
				Subsystem: "test",
				Status:    integtest.ScenarioPass,
				Detail:    "custom scenario ran",
			}
		},
	})
	r.Run(context.Background())
	if !called {
		t.Error("custom scenario was not called")
	}
}

func TestMatrixReportCounts(t *testing.T) {
	r := integtest.New("", "")
	r.Register(
		&integtest.Scenario{
			Name: "will-pass", Subsystem: "test",
			Run: func(ctx context.Context) integtest.ScenarioResult {
				return integtest.ScenarioResult{Name: "will-pass", Status: integtest.ScenarioPass}
			},
		},
		&integtest.Scenario{
			Name: "will-skip", Subsystem: "test",
			Run: func(ctx context.Context) integtest.ScenarioResult {
				return integtest.ScenarioResult{Name: "will-skip", Status: integtest.ScenarioSkip}
			},
		},
		&integtest.Scenario{
			Name: "will-fail", Subsystem: "test",
			Run: func(ctx context.Context) integtest.ScenarioResult {
				return integtest.ScenarioResult{Name: "will-fail", Status: integtest.ScenarioFail, Detail: "injected failure"}
			},
		},
	)
	rep := r.Run(context.Background())

	// Counts must include built-in scenarios too; verify min values.
	if rep.Pass < 1 {
		t.Errorf("Pass = %d; want >= 1", rep.Pass)
	}
	if rep.Skip < 1 {
		t.Errorf("Skip = %d; want >= 1", rep.Skip)
	}
	if rep.Fail < 1 {
		t.Errorf("Fail = %d; want >= 1", rep.Fail)
	}
	if rep.Overall != "fail" {
		t.Errorf("Overall = %q; want \"fail\" when there is a failing scenario", rep.Overall)
	}
}

func TestMatrixReportOverallPass(t *testing.T) {
	r := integtest.New("", "")
	rep := r.Run(context.Background())
	// Without a gateway there are no failures in the base matrix.
	if rep.Fail > 0 {
		for _, res := range rep.Results {
			if res.Status == integtest.ScenarioFail {
				t.Logf("failing scenario: %s -- %s", res.Name, res.Detail)
			}
		}
		t.Errorf("base matrix with no gateway: Fail=%d; want 0", rep.Fail)
	}
	if rep.Overall != "pass" {
		t.Errorf("Overall = %q; want \"pass\"", rep.Overall)
	}
}

// --- helpers ---

func fakeGateway(code int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
	}))
}

func isGatewayScenario(name string) bool {
	switch name {
	case "gateway-healthz", "gateway-version", "gateway-status", "gateway-metrics", "provider-health-snapshot":
		return true
	}
	return false
}
