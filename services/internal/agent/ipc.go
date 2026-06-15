// Package agent defines the IPC contract between the Go gateway and the Rust
// nura-agent.
//
// The agent listens on a Unix domain socket (/run/nura-agent.sock) and speaks
// a simple JSON-over-HTTP protocol. The gateway translates incoming HTTP
// requests from external clients into calls on this socket.
//
// Phase 28 defines the contract types. Phase 29 implements the client.
package agent

import "time"

// SocketPath is the Unix domain socket path for the Rust agent IPC.
// Both the agent (server) and gateway (client) must agree on this path.
const SocketPath = "/run/nura-agent.sock"

// DefaultDialTimeout is the maximum time to wait when connecting to the socket.
const DefaultDialTimeout = 500 * time.Millisecond

// HealthResponse is returned by GET /health on the agent socket.
type HealthResponse struct {
	Status   string `json:"status"`           // "ok" | "starting" | "error"
	Provider string `json:"provider"`          // active provider name
	Uptime   int64  `json:"uptime_seconds"`    // seconds since agent started
}

// TurnRequest is sent to POST /turns on the agent socket (Phase 29).
// The gateway translates POST /chat requests into this shape.
type TurnRequest struct {
	Messages       []Message `json:"messages"`
	MaxTokens      int       `json:"max_tokens,omitempty"`
	Temperature    float32   `json:"temperature,omitempty"`
	ProviderHint   string    `json:"provider_hint,omitempty"` // optional; overrides routing
	StreamResponse bool      `json:"stream"`
}

// Message is a single conversation turn sent in a TurnRequest.
type Message struct {
	Role    string `json:"role"`    // "system" | "user" | "assistant"
	Content string `json:"content"`
}

// TurnEvent is one SSE frame streamed back from POST /turns (Phase 29).
type TurnEvent struct {
	Type    string `json:"type"`             // "token" | "usage" | "done" | "error"
	Text    string `json:"text,omitempty"`
	Message string `json:"message,omitempty"` // error message when Type == "error"
}

// ToolsResponse is returned by GET /tools on the agent socket (Phase 29).
type ToolsResponse struct {
	Tools []ToolInfo `json:"tools"`
}

// ToolInfo describes one tool visible to the model.
type ToolInfo struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	ReadOnly    bool        `json:"read_only"`
	Schema      interface{} `json:"schema"`
}
