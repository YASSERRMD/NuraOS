package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPClient is bound to a guest gateway base URL and optionally carries
// a Bearer token for authenticated suites.
type HTTPClient struct {
	BaseURL string
	Token   string
}

// Get performs a GET request to path and returns the response.
// The caller is responsible for closing resp.Body.
func (h *HTTPClient) Get(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, h.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if h.Token != "" {
		req.Header.Set("Authorization", "Bearer "+h.Token)
	}
	return h.do(req)
}

// PostJSON performs a POST request with body marshalled as JSON.
// The caller is responsible for closing resp.Body.
func (h *HTTPClient) PostJSON(path string, body interface{}) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling request body: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, h.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.Token != "" {
		req.Header.Set("Authorization", "Bearer "+h.Token)
	}
	return h.do(req)
}

// GetBody performs a GET and returns the full response body as a string.
func (h *HTTPClient) GetBody(path string) (int, string, error) {
	resp, err := h.Get(path)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(b), nil
}

func (h *HTTPClient) do(req *http.Request) (*http.Response, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	return client.Do(req)
}
