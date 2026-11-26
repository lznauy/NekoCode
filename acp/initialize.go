package acp

import (
	"encoding/json"

	"nekocode/util/version"
)

// initializeRequest mirrors the InitializeRequest params. Only the fields the
// agent reacts to are modeled; everything else is ignored per protocol.
type initializeRequest struct {
	ProtocolVersion    int             `json:"protocolVersion"`
	ClientCapabilities json.RawMessage `json:"clientCapabilities"`
}

// clientSupportsBooleanConfig inspects whether the client advertised support
// for boolean session config options. The schema marks every capability
// field x-deserialize-default-on-error, so malformed payloads must degrade
// to "not advertised" instead of failing the handshake.
func clientSupportsBooleanConfig(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var caps struct {
		Session *struct {
			ConfigOptions *struct {
				Boolean json.RawMessage `json:"boolean"`
			} `json:"configOptions"`
		} `json:"session"`
	}
	if json.Unmarshal(raw, &caps) != nil || caps.Session == nil || caps.Session.ConfigOptions == nil {
		return false
	}
	flag := caps.Session.ConfigOptions.Boolean
	if len(flag) == 0 || string(flag) == "null" || string(flag) == "false" {
		return false
	}
	var object struct{}
	return json.Unmarshal(flag, &object) == nil
}

func (s *server) initialize(params json.RawMessage) (any, *wireError) {
	var request initializeRequest
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, rpcError(-32602, "invalid initialize params: %v", err)
	}
	if request.ProtocolVersion <= 0 {
		return nil, rpcError(-32602, "protocolVersion is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initialized {
		return nil, rpcError(-32600, "connection is already initialized")
	}
	s.initialized = true
	s.clientBooleanConfig = clientSupportsBooleanConfig(request.ClientCapabilities)
	return initializeResponse{
		ProtocolVersion: protocolVersion,
		AgentCapabilities: agentCapabilities{
			LoadSession:         true,
			PromptCapabilities:  promptCapabilities{Image: true},
			MCPCapabilities:     mcpCapabilities{},
			SessionCapabilities: sessionCapabilities{List: &struct{}{}, Delete: &struct{}{}},
		},
		AgentInfo:   implementation{Name: "nekocode", Title: "NekoCode", Version: version.Version},
		AuthMethods: []any{},
	}, nil
}
