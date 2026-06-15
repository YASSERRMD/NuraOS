package harness

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// WaitReady polls the guest /healthz endpoint until it returns HTTP 200 or
// the timeout elapses. It never uses fixed sleeps; each failed attempt waits
// a short interval before the next poll.
func WaitReady(ctx context.Context, inst *QEMUInstance, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", inst.APIPort)
	client := &http.Client{Timeout: 2 * time.Second}

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("guest did not become ready within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := client.Get(url) //nolint:noctx
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		// Short delay before next poll. Using select so we honour context
		// cancellation without a fixed sleep.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
