package headless

import (
	"context"

	rt "nekocode/runtime"
)

// Discovery projects backend capabilities and safe metadata onto the wire.
func (s *server) capabilities() []string {
	capabilities := append(capabilities(), "server.info", "shutdown")
	if m, ok := s.backend.(Management); ok {
		c := m.Capabilities()
		if c.ModelCatalog {
			capabilities = append(capabilities, "models.list")
		}
		if c.ModelSelection {
			capabilities = append(capabilities, "model.set")
		}
		if c.PermissionControl {
			capabilities = append(capabilities, "permissions.set")
		}
		if c.Sessions {
			capabilities = append(capabilities, "sessions.list", "session.history")
		}
		if c.SessionCreate {
			capabilities = append(capabilities, "session.new")
		}
		if c.SessionResume {
			capabilities = append(capabilities, "session.resume")
		}
		if c.SessionDelete {
			capabilities = append(capabilities, "session.delete")
		}
		if c.Extensions {
			capabilities = append(capabilities, "extensions.list")
		}
		if c.Checkpoints {
			capabilities = append(capabilities, "workspace.checkpoints")
		}
		if c.Rewind {
			capabilities = append(capabilities, "workspace.rewind")
		}
		if c.Steering {
			capabilities = append(capabilities, "run.steer")
		}
	}
	return capabilities
}

func (s *server) metadata(ctx context.Context) map[string]any {
	info := map[string]any{"protocol_version": ProtocolVersion, "capabilities": s.capabilities(), "cwd": s.cwd, "session_id": s.sessionID, "model": s.backend.CurrentModel(), "permission_mode": s.backend.PermissionMode()}
	if m, ok := s.backend.(Management); ok {
		c := m.Capabilities()
		if c.ToolCatalog {
			info["tools"] = m.ToolNames()
		}
		if c.Commands {
			if menu, ok := m.CommandMenu(ctx, "/"); ok {
				info["commands"] = menu.Items
			}
		}
	}
	return info
}

func extensionSummary(view rt.SkillManagementView) map[string]any {
	// Deliberately omit process arguments, environment, paths and config keys.
	skills := []map[string]any{}
	plugins := []map[string]any{}
	servers := []map[string]any{}
	for _, v := range view.Skills {
		skills = append(skills, map[string]any{"name": v.Name, "description": v.Description, "loaded": v.Loaded})
	}
	for _, v := range view.Plugins {
		plugins = append(plugins, map[string]any{"name": v.Name, "version": v.Version, "enabled": v.Enabled})
	}
	for _, v := range view.MCP {
		servers = append(servers, map[string]any{"name": v.Name, "status": v.Status, "tool_count": v.ToolCount})
	}
	return map[string]any{"skills": skills, "plugins": plugins, "mcp_servers": servers}
}
