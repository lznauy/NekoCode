package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ToolIdentity is a concrete server/tool pair, never a wildcard or display line.
type ToolIdentity struct{ Server, Tool string }

type capturedTool struct {
	identity   ToolIdentity
	server     *server
	definition toolDef
}

// ToolSnapshot binds exact ready tool definitions to their original connection.
// It shares a lifecycle-managed service, not main Agent state. Reconfiguration
// invalidates the binding rather than redirecting old authority to new code.
type ToolSnapshot struct {
	manager *Manager
	tools   []capturedTool
}

func (m *Manager) CaptureTools() *ToolSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := &ToolSnapshot{manager: m}
	if m.closed {
		return out
	}
	for _, s := range m.servers {
		if m.health[s.name].Status != StatusReady {
			continue
		}
		for _, def := range s.tools {
			// Detach nested schemas from any future discovery refresh.
			data, _ := json.Marshal(def)
			var copy toolDef
			_ = json.Unmarshal(data, &copy)
			out.tools = append(out.tools, capturedTool{identity: ToolIdentity{s.name, def.Name}, server: s, definition: copy})
		}
	}
	sort.Slice(out.tools, func(i, j int) bool {
		a, b := out.tools[i].identity, out.tools[j].identity
		if a.Server == b.Server {
			return a.Tool < b.Tool
		}
		return a.Server < b.Server
	})
	return out
}

func (s *ToolSnapshot) Identities() []ToolIdentity {
	out := make([]ToolIdentity, 0, len(s.tools))
	for _, tool := range s.tools {
		out = append(out, tool.identity)
	}
	return out
}

// Filter returns another immutable view and cannot introduce new targets.
func (s *ToolSnapshot) Filter(allow func(ToolIdentity) bool) *ToolSnapshot {
	out := &ToolSnapshot{manager: s.manager}
	for _, tool := range s.tools {
		if allow(tool.identity) {
			out.tools = append(out.tools, tool)
		}
	}
	return out
}
func (s *ToolSnapshot) find(serverName, toolName string) (capturedTool, error) {
	for _, tool := range s.tools {
		if tool.identity.Server == serverName && tool.identity.Tool == toolName {
			s.manager.mu.Lock()
			valid := !s.manager.closed && s.manager.servers[s.manager.owners[serverName]] == tool.server
			s.manager.mu.Unlock()
			if !valid {
				return capturedTool{}, fmt.Errorf("captured MCP server %q was replaced or closed; submit a new task", serverName)
			}
			return tool, nil
		}
	}
	return capturedTool{}, fmt.Errorf("MCP tool %q/%q is outside the captured task capability set", serverName, toolName)
}
func (s *ToolSnapshot) ListCapabilities() string {
	var out strings.Builder
	for _, tool := range s.tools {
		if _, err := s.find(tool.identity.Server, tool.identity.Tool); err != nil {
			continue
		}
		fmt.Fprintf(&out, "%s: %s\n", tool.identity.Server, toolLine(tool.definition))
	}
	if out.Len() == 0 {
		return "No MCP tools in this task's captured capability set."
	}
	return strings.TrimSpace(out.String())
}
func (s *ToolSnapshot) InspectTool(serverName, toolName string) (string, error) {
	tool, err := s.find(serverName, toolName)
	if err != nil {
		return "", err
	}
	schema, err := json.MarshalIndent(tool.definition.InputSchema, "", "  ")
	if err != nil {
		return "", err
	}
	return serverName + "/" + toolName + "\n" + tool.definition.Description + "\n" + string(schema), nil
}
func (s *ToolSnapshot) CallServerTool(ctx context.Context, serverName, toolName string, args map[string]any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	tool, err := s.find(serverName, toolName)
	if err != nil {
		return "", err
	}
	return tool.server.client.CallTool(ctx, tool.definition.Name, args)
}
