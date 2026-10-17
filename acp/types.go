// Package acp exposes NekoCode as an Agent Client Protocol v1 agent.
package acp

import (
	"encoding/json"
	"fmt"
)

const protocolVersion = 1

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *wireError      `json:"error,omitempty"`
}

type wireError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type implementation struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

type initializeResponse struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities agentCapabilities `json:"agentCapabilities"`
	AgentInfo         implementation    `json:"agentInfo"`
	AuthMethods       []any             `json:"authMethods"`
}

type agentCapabilities struct {
	LoadSession         bool                `json:"loadSession"`
	PromptCapabilities  promptCapabilities  `json:"promptCapabilities"`
	MCPCapabilities     mcpCapabilities     `json:"mcpCapabilities"`
	SessionCapabilities sessionCapabilities `json:"sessionCapabilities"`
}

type promptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

type mcpCapabilities struct {
	HTTP bool `json:"http"`
	SSE  bool `json:"sse"`
}

type sessionCapabilities struct {
	List   *struct{} `json:"list,omitempty"`
	Delete *struct{} `json:"delete,omitempty"`
}

type sessionRequest struct {
	SessionID             string         `json:"sessionId,omitempty"`
	CWD                   string         `json:"cwd"`
	MCPServers            []mcpServerRef `json:"mcpServers"`
	AdditionalDirectories []string       `json:"additionalDirectories,omitempty"`
}

// mcpServerRef mirrors the ACP McpServer union fields this agent consumes.
// Type is empty for the default stdio variant; http/sse entries are rejected
// by validation, so their url/headers payloads are never inspected.
type mcpServerRef struct {
	Type    string      `json:"type"`
	Name    string      `json:"name"`
	Command string      `json:"command"`
	Args    []string    `json:"args"`
	Env     []mcpEnvVar `json:"env"`
}

type mcpEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type promptRequest struct {
	SessionID string         `json:"sessionId"`
	Prompt    []contentBlock `json:"prompt"`
}

type contentBlock struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	Name        string `json:"name,omitempty"`
	URI         string `json:"uri,omitempty"`
	Description string `json:"description,omitempty"`
	// Data carries the base64 image payload for "image" blocks (ACP spec).
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type permissionResponse struct {
	Outcome struct {
		Outcome  string `json:"outcome"`
		OptionID string `json:"optionId,omitempty"`
	} `json:"outcome"`
}

func rpcError(code int, format string, args ...any) *wireError {
	return &wireError{Code: code, Message: fmt.Sprintf(format, args...)}
}
