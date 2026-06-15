package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Client is an HTTP client that communicates with nura-agent over a Unix
// domain socket using plain HTTP/1.1. All request methods accept a context
// for cancellation; cancelling the context closes the underlying connection
// and signals the agent to abort any in-progress turn.
type Client struct {
	http    *http.Client
	baseURL string
}

// New creates a Client for socketPath. dialTimeout bounds connection setup
// only; use request contexts to control full request lifetime.
func New(socketPath string, dialTimeout time.Duration) *Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: dialTimeout}).DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		http:    &http.Client{Transport: tr},
		baseURL: "http://unix",
	}
}

// Health calls GET /health and parses the agent's health response.
func (c *Client) Health(ctx context.Context) (HealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return HealthResponse{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return HealthResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return HealthResponse{}, fmt.Errorf("agent /health returned %d", resp.StatusCode)
	}
	var h HealthResponse
	return h, json.NewDecoder(resp.Body).Decode(&h)
}

// ChatStream sends req to POST /turns and returns the raw HTTP response.
// The caller must close resp.Body when done. Cancelling ctx closes the
// underlying connection, which the agent interprets as a turn cancellation.
func (c *Client) ChatStream(ctx context.Context, req TurnRequest) (*http.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/turns", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	return c.http.Do(httpReq)
}

// Tools calls GET /tools and parses the agent's tool list.
func (c *Client) Tools(ctx context.Context) (ToolsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/tools", nil)
	if err != nil {
		return ToolsResponse{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ToolsResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ToolsResponse{}, fmt.Errorf("agent /tools returned %d", resp.StatusCode)
	}
	var t ToolsResponse
	return t, json.NewDecoder(resp.Body).Decode(&t)
}

// Metrics calls GET /metrics on the agent socket and returns the agent's
// operational counters. Returns an error if the agent is unreachable or
// does not yet expose this endpoint; callers should degrade gracefully.
func (c *Client) Metrics(ctx context.Context) (AgentMetrics, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/metrics", nil)
	if err != nil {
		return AgentMetrics{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return AgentMetrics{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return AgentMetrics{}, fmt.Errorf("agent /metrics returned %d", resp.StatusCode)
	}
	var m AgentMetrics
	return m, json.NewDecoder(resp.Body).Decode(&m)
}
