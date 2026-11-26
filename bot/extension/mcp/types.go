package mcp

import "encoding/json"

// ServerConfig defines how to launch an MCP server.
type ServerConfig struct {
	URL                    string            `json:"url,omitempty"`
	AllowInsecureHTTP      bool              `json:"allow_insecure_http,omitempty"`
	Headers                map[string]string `json:"headers,omitempty"`
	OAuthClientID          string            `json:"oauth_client_id,omitempty"`
	OAuthClientSecret      string            `json:"oauth_client_secret,omitempty"`
	OAuthClientMetadataURL string            `json:"oauth_client_metadata_url,omitempty"`
	OAuthCallbackPort      int               `json:"oauth_callback_port,omitempty"`
	interactive            bool
	authorizationScopes    []string
	CWD                    string            `json:"cwd,omitempty"`
	Command                string            `json:"command"`
	Args                   []string          `json:"args,omitempty"`
	Env                    map[string]string `json:"env,omitempty"`
}

// Registration binds a lifecycle owner ID and user-visible name to one MCP
// server configuration. Replace uses registrations as an atomic set.
type Registration struct {
	ID     string
	Name   string
	Config ServerConfig
}

// toolDef represents a tool discovered from an MCP server.
type toolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonrpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}
