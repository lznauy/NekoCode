package app

import "nekocode/common"

func (e *extensionFacade) SkillManagementView() common.SkillManagementView {
	mcpServers := e.plugins.MCPServers()
	mcpServers = append(mcpServers, e.configMCP...)
	e.applyMCPHealth(mcpServers)
	return e.skills.ManagementView(e.plugins.Views(), mcpServers)
}

func (e *extensionFacade) applyMCPHealth(servers []common.MCPServerView) {
	for i := range servers {
		if !servers[i].PluginEnabled {
			servers[i].Status = "disabled"
			continue
		}
		health, ok := e.mcpHealth[servers[i].Name]
		if !ok {
			servers[i].Status = "unknown"
			continue
		}
		servers[i].Status = health.Status
		servers[i].Error = health.Error
		servers[i].ToolCount = health.ToolCount
	}
}

func (e *extensionFacade) SetPluginEnabled(name string, enabled bool) (common.SkillManagementView, error) {
	if _, err := e.plugins.SetEnabled(name, enabled); err != nil {
		return common.SkillManagementView{}, err
	}
	return e.SkillManagementView(), nil
}

func (e *extensionFacade) RefreshSkillManagement() common.SkillManagementView {
	e.plugins.Reload()
	return e.SkillManagementView()
}
