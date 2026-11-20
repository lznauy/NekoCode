package acp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"nekocode/util/fs"
	"path/filepath"
	"strings"
	"time"

	controlruntime "nekocode/runtime"
)

// maxReplayImageBytes caps image data replayed through session/load so a
// history with many large generated images cannot flood the client stream.
const maxReplayImageBytes = 8 << 20

const maxMCPServers = 16
const maxMCPConfigBytes = 64 << 10

// mcpSource namespaces MCP servers registered from ACP session setup requests.
const mcpSource = "acp"

func (s *server) newSession(ctx context.Context, params json.RawMessage) (any, *wireError) {
	request, rpcErr := s.decodeSessionRequest(params, false)
	if rpcErr != nil {
		return nil, rpcErr
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.hasActiveTurn() {
		return nil, rpcError(-32000, "cannot create a session while a prompt is running")
	}
	previousID := s.backend.CurrentSessionID()
	session, err := s.backend.NewSession()
	if err != nil {
		return nil, backendError("create session", err)
	}
	if rpcErr := s.finishSessionSetup(ctx, session.ID, request); rpcErr != nil {
		s.rollbackSession(previousID)
		return nil, rpcErr
	}
	return map[string]any{"sessionId": session.ID, "configOptions": s.configOptions()}, nil
}

func (s *server) loadSession(ctx context.Context, params json.RawMessage) (any, *wireError) {
	request, rpcErr := s.decodeSessionRequest(params, true)
	if rpcErr != nil {
		return nil, rpcErr
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.hasActiveTurn() {
		return nil, rpcError(-32000, "cannot load a session while a prompt is running")
	}
	if !s.findWorkspaceSession(request.SessionID) {
		return nil, rpcError(-32602, "unknown session %q", request.SessionID)
	}
	previousID := s.backend.CurrentSessionID()
	if err := s.backend.ResumeSession(request.SessionID); err != nil {
		return nil, backendError("load session", err)
	}
	if rpcErr := s.finishSessionSetup(ctx, request.SessionID, request); rpcErr != nil {
		s.rollbackSession(previousID)
		return nil, rpcErr
	}
	for index, item := range s.backend.SessionMessages() {
		for _, update := range historyUpdates(ctx, index, item) {
			if err := s.notifySession(request.SessionID, update); err != nil {
				return nil, backendError("replay session", err)
			}
			if err := ctx.Err(); err != nil {
				return nil, backendError("load session", err)
			}
		}
	}
	// The session-usage RFD asks agents to report context occupancy right
	// after a client (re)connects to an existing session, so the usage
	// indicator is populated before the next turn.
	if update := usageUpdate(s.backend.Metrics()); update != nil {
		if err := s.notifySession(request.SessionID, update); err != nil {
			return nil, backendError("replay session", err)
		}
	}
	return map[string]any{"configOptions": s.configOptions()}, nil
}

// historyUpdates projects one persisted display message onto ACP session
// updates: reasoning, tool blocks, text content and images are replayed so a
// loaded session matches what the live stream would have delivered.
func historyUpdates(ctx context.Context, index int, item controlruntime.DisplayMessage) []map[string]any {
	messageID := fmt.Sprintf("history-%d", index)
	var updates []map[string]any
	chunk := func(kind string, content map[string]any) map[string]any {
		return map[string]any{"sessionUpdate": kind, "messageId": messageID, "content": content}
	}
	switch item.Role {
	case "user":
		if item.Content != "" {
			updates = append(updates, chunk("user_message_chunk", map[string]any{"type": "text", "text": item.Content}))
		}
	case "assistant":
		if item.Reasoning != "" {
			updates = append(updates, chunk("agent_thought_chunk", map[string]any{"type": "text", "text": item.Reasoning}))
		}
		for blockIndex, block := range item.Blocks {
			callID := fmt.Sprintf("%s-tool-%d", messageID, blockIndex)
			status := "completed"
			if block.IsError {
				status = "failed"
			}
			updates = append(updates, map[string]any{
				"sessionUpdate": "tool_call",
				"toolCallId":    callID,
				"title":         block.ToolName,
				"kind":          toolKind(block.ToolName),
				"status":        status,
				"rawInput":      rawValue(block.Args),
			})
			if block.Content != "" {
				updates = append(updates, toolUpdateByID(callID, status, block.Content))
			}
		}
		if item.Content != "" {
			updates = append(updates, chunk("agent_message_chunk", map[string]any{"type": "text", "text": item.Content}))
		}
		for _, ref := range item.Images {
			if content := imageContent(ctx, ref); content != nil {
				updates = append(updates, chunk("agent_message_chunk", content))
			}
		}
	}
	return updates
}

func (s *server) listSessions(params json.RawMessage) (any, *wireError) {
	var request struct {
		CWD    string  `json:"cwd"`
		Cursor *string `json:"cursor"`
	}
	if len(params) != 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, rpcError(-32602, "invalid session/list params: %v", err)
		}
	}
	if request.Cursor != nil && *request.Cursor != "" {
		return nil, rpcError(-32602, "unknown session cursor")
	}
	if request.CWD != "" && !filepath.IsAbs(request.CWD) {
		return nil, rpcError(-32602, "cwd must be absolute")
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	items := make([]map[string]any, 0)
	for _, session := range s.backend.ListSessions() {
		if filepath.Clean(session.CWD) != s.cwd || request.CWD != "" && filepath.Clean(request.CWD) != filepath.Clean(session.CWD) {
			continue
		}
		items = append(items, map[string]any{
			"sessionId": session.ID,
			"cwd":       session.CWD,
			"updatedAt": time.Unix(session.UpdatedAt, 0).UTC().Format(time.RFC3339),
		})
	}
	return map[string]any{"sessions": items}, nil
}

func (s *server) deleteSession(ctx context.Context, params json.RawMessage) (any, *wireError) {
	var request struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(params, &request); err != nil || request.SessionID == "" {
		return nil, rpcError(-32602, "invalid session/delete params")
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.hasActiveTurn() {
		return nil, rpcError(-32000, "cannot delete a session while a prompt is running")
	}
	if !s.findWorkspaceSession(request.SessionID) {
		return nil, rpcError(-32602, "unknown session %q", request.SessionID)
	}
	if err := s.backend.DeleteSession(request.SessionID); err != nil {
		return nil, backendError("delete session", err)
	}
	if s.backend.CurrentSessionID() == "" {
		if err := s.backend.ReplaceMCPServers(ctx, mcpSource, nil); err != nil {
			return nil, backendError("clear session MCP servers", err)
		}
		s.activeMCPSession = ""
	}
	delete(s.sessionConfigs, request.SessionID)
	delete(s.sessionMCPConfigs, request.SessionID)
	return struct{}{}, nil
}

// findWorkspaceSession reports whether a persisted session with this ID
// belongs to the agent workspace. Unlike verifySession it does not accept the
// active-but-unpersisted session: load and delete need the on-disk record.
func (s *server) findWorkspaceSession(sessionID string) bool {
	meta, ok := s.findSession(sessionID)
	return ok && filepath.Clean(meta.CWD) == s.cwd
}

func (s *server) decodeSessionRequest(params json.RawMessage, requireID bool) (sessionRequest, *wireError) {
	var request sessionRequest
	if err := json.Unmarshal(params, &request); err != nil {
		return request, rpcError(-32602, "invalid session params: %v", err)
	}
	if !filepath.IsAbs(request.CWD) || filepath.Clean(request.CWD) != s.cwd {
		return request, rpcError(-32602, "cwd must be the absolute agent working directory %q", s.cwd)
	}
	if requireID && request.SessionID == "" {
		return request, rpcError(-32602, "sessionId is required")
	}
	if rpcErr := validateMCPServers(request.MCPServers); rpcErr != nil {
		return request, rpcErr
	}
	if len(request.MCPServers) > 0 && !s.allowClientMCP {
		return request, rpcError(-32602, "client-supplied MCP servers are disabled; start with --allow-client-mcp only for trusted clients")
	}
	if len(request.AdditionalDirectories) != 0 {
		return request, rpcError(-32602, "additionalDirectories are not supported")
	}
	return request, nil
}

// validateMCPServers checks the client-supplied server list. stdio is the ACP
// v1 baseline transport and must be accepted; http and sse stay behind the
// mcpCapabilities declaration, which this agent leaves off.
func validateMCPServers(servers []mcpServerRef) *wireError {
	if len(servers) > maxMCPServers {
		return rpcError(-32602, "at most %d MCP servers are allowed", maxMCPServers)
	}
	seen := make(map[string]struct{}, len(servers))
	totalBytes := 0
	for index, server := range servers {
		switch server.Type {
		case "", "stdio":
			if server.Name == "" || server.Command == "" {
				return rpcError(-32602, "mcpServers[%d]: stdio server requires name and command", index)
			}
		case "http", "sse":
			return rpcError(-32602, "mcpServers[%d]: %s MCP servers are not supported", index, server.Type)
		default:
			return rpcError(-32602, "mcpServers[%d]: unknown MCP server type %q", index, server.Type)
		}
		if _, exists := seen[server.Name]; exists {
			return rpcError(-32602, "mcpServers[%d]: duplicate server name %q", index, server.Name)
		}
		seen[server.Name] = struct{}{}
		totalBytes += len(server.Name) + len(server.Command)
		for _, arg := range server.Args {
			totalBytes += len(arg)
		}
		for _, variable := range server.Env {
			totalBytes += len(variable.Name) + len(variable.Value)
		}
		if totalBytes > maxMCPConfigBytes {
			return rpcError(-32602, "MCP server configuration exceeds %d bytes", maxMCPConfigBytes)
		}
	}
	return nil
}

// applyMCPServers replaces the MCP servers previously registered for ACP with
// the client-supplied list. It must run without an active turn.
func (s *server) applyMCPServers(ctx context.Context, sessionID string, request sessionRequest) *wireError {
	if s.sessionMCPConfigs == nil {
		s.sessionMCPConfigs = make(map[string][]controlruntime.MCPServerSpec)
	}
	if len(request.MCPServers) > 0 && !s.allowClientMCP {
		return rpcError(-32602, "client-supplied MCP servers are disabled; start with --allow-client-mcp only for trusted clients")
	}
	servers := make([]controlruntime.MCPServerSpec, 0, len(request.MCPServers))
	for _, server := range request.MCPServers {
		env := make(map[string]string, len(server.Env))
		for _, variable := range server.Env {
			env[variable.Name] = variable.Value
		}
		config := controlruntime.MCPServerConfig{
			Command: server.Command,
			Args:    append([]string(nil), server.Args...),
			Env:     env,
			Enabled: true,
		}
		servers = append(servers, controlruntime.MCPServerSpec{Name: server.Name, Config: config})
	}
	if err := s.backend.ReplaceMCPServers(ctx, mcpSource, servers); err != nil {
		return backendError("replace MCP servers", err)
	}
	s.sessionMCPConfigs[sessionID] = cloneMCPServerSpecs(servers)
	s.activeMCPSession = sessionID
	return nil
}

func (s *server) finishSessionSetup(ctx context.Context, sessionID string, request sessionRequest) *wireError {
	if err := s.applySessionConfig(sessionID); err != nil {
		return backendError("apply session config", err)
	}
	return s.applyMCPServers(ctx, sessionID, request)
}

func (s *server) rollbackSession(previousID string) {
	if previousID == "" {
		return
	}
	if err := s.backend.ResumeSession(previousID); err != nil {
		return
	}
	_ = s.applySessionConfig(previousID)
}

func cloneMCPServerSpecs(servers []controlruntime.MCPServerSpec) []controlruntime.MCPServerSpec {
	cloned := make([]controlruntime.MCPServerSpec, len(servers))
	for i, server := range servers {
		cloned[i] = server
		cloned[i].Config.Args = append([]string(nil), server.Config.Args...)
		cloned[i].Config.Env = make(map[string]string, len(server.Config.Env))
		for key, value := range server.Config.Env {
			cloned[i].Config.Env[key] = value
		}
	}
	return cloned
}

func (s *server) findSession(id string) (controlruntime.SessionMeta, bool) {
	for _, item := range s.backend.ListSessions() {
		if item.ID == id {
			return item, true
		}
	}
	return controlruntime.SessionMeta{}, false
}

func (s *server) hasActiveTurn() bool {
	s.mu.Lock()
	// The admission path holds sessionMu, so another turn cannot be admitted
	// while we check a previous prompt whose cleanup deadline elapsed.
	for id, turn := range s.active {
		if !turn.awaitingSettlement {
			s.mu.Unlock()
			return true
		}
		runID := turn.runID
		s.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		err := s.backend.WaitRun(ctx, runID)
		cancel()
		if err != nil {
			return true
		}
		s.mu.Lock()
		delete(s.active, id)
	}
	s.mu.Unlock()
	return false
}

// imageContent converts a persisted image reference into an ACP image content
// block. Images that cannot be read (or exceed the replay cap) are skipped.
func imageContent(ctx context.Context, ref controlruntime.ImageRef) map[string]any {
	mime := imageMIME(ref.Path)
	if mime == "" {
		return nil
	}
	data, err := fs.ReadRegularFile(ctx, ref.Path, maxReplayImageBytes)
	if err != nil || len(data) == 0 {
		return nil
	}
	content := map[string]any{
		"type":     "image",
		"data":     base64.StdEncoding.EncodeToString(data),
		"mimeType": mime,
	}
	if ref.URL != "" {
		content["uri"] = ref.URL
	}
	return content
}

func imageMIME(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}
