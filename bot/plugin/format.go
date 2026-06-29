package plugin

import (
	"fmt"
	"strings"
)

func FormatInstallPreview(p *Plugin) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Plugin: %s", p.Name)
	if p.Version != "" {
		fmt.Fprintf(&sb, " v%s", p.Version)
	}
	if p.Description != "" {
		fmt.Fprintf(&sb, "\n%s", p.Description)
	}
	skillDirs := p.SkillDirs()
	agentPaths := p.AgentPaths()
	mcpCount := len(p.MCPServers())
	fmt.Fprintf(&sb, "\nSkills: %d  Agents: %d  MCP: %d", len(skillDirs), len(agentPaths), mcpCount)
	if p.HasInstallStub {
		sb.WriteString("\n[!] install.sh detected")
	}
	return sb.String()
}

func FormatInstallResult(p *Plugin) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Installed plugin %q v%s.\n", p.Name, p.Version)
	if p.HasInstallStub {
		sb.WriteString("Note: This plugin has an install.sh script. Run it manually if needed:\n")
		fmt.Fprintf(&sb, "  cd %s && bash install.sh\n", p.Dir)
	}
	agentCount := len(p.AgentPaths())
	fmt.Fprintf(&sb, "Skills: %d  Agents: %d  MCP: %d", len(p.SkillDirs()), agentCount, len(p.MCPServers()))
	return sb.String()
}

func FormatList(plugins []*Plugin) string {
	if len(plugins) == 0 {
		return "No plugins installed. Use /plugin install <source> to add one."
	}
	var sb strings.Builder
	sb.WriteString("Installed plugins:\n")
	for _, p := range plugins {
		status := "+"
		if !p.Enabled {
			status = "-"
		}
		ver := p.Version
		if ver == "" {
			ver = "-"
		}
		fmt.Fprintf(&sb, "  %s %-30s v%-6s %s\n", status, p.Name, ver, p.Dir)
	}
	return sb.String()
}

func FormatInfo(p *Plugin) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Name:        %s\n", p.Name)
	fmt.Fprintf(&sb, "Version:     %s\n", p.Version)
	fmt.Fprintf(&sb, "Description: %s\n", p.Description)
	fmt.Fprintf(&sb, "Directory:   %s\n", p.Dir)
	if p.Source != "" {
		fmt.Fprintf(&sb, "Source:      %s\n", p.Source)
	}
	fmt.Fprintf(&sb, "Enabled:     %v\n", p.Enabled)
	fmt.Fprintf(&sb, "Skills:      %d dirs\n", len(p.SkillDirs()))
	fmt.Fprintf(&sb, "Agents:      %d\n", len(p.AgentPaths()))
	fmt.Fprintf(&sb, "Commands:    %d\n", len(p.Commands))
	fmt.Fprintf(&sb, "MCP Servers: %d\n", len(p.MCPServers()))
	if p.HasInstallStub {
		sb.WriteString("install.sh:  present (not yet executed)\n")
	}
	return sb.String()
}
