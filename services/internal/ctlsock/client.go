package ctlsock

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Client communicates with a nura-manager control socket.
type Client struct {
	path string
}

// NewClient returns a Client targeting path (default: SocketPath).
func NewClient(path string) *Client {
	if path == "" {
		path = SocketPath
	}
	return &Client{path: path}
}

// Send sends one request and returns the response.
func (c *Client) Send(req Request) (Response, error) {
	conn, err := net.DialTimeout("unix", c.path, 3*time.Second)
	if err != nil {
		return Response{}, fmt.Errorf("connect to manager socket %s: %w", c.path, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		return Response{}, fmt.Errorf("send request: %w", err)
	}

	var resp Response
	dec := json.NewDecoder(conn)
	if err := dec.Decode(&resp); err != nil {
		return Response{}, fmt.Errorf("decode response: %w", err)
	}
	return resp, nil
}
