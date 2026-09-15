package extension

import (
	"context"
	"fmt"
	"strings"

	"nekocode/bot/command"
	"nekocode/bot/extension/plugin"
	"nekocode/protocol"
)

type InstallConfirm func(source string, plugin *plugin.Plugin, remote bool) bool

// RegisterCommands installs the /plugin command family.
func (m *Manager) RegisterCommands(handler *command.Handler, confirm InstallConfirm) {
	m.mu.Lock()
	m.commands = handler
	m.syncSkillCommandsLocked()
	m.mu.Unlock()
	p := handler.Parser()
	p.RegisterInfo("plugin", "Manage plugins", func(ctx context.Context, cmd *command.Command) (string, bool) {
		if len(cmd.Args) == 0 {
			return plugin.Usage(), true
		}
		switch cmd.Args[0] {
		case "install":
			return m.installPlugin(ctx, cmd.Args[1:], confirm), true
		case "uninstall":
			return m.uninstallPlugin(ctx, cmd.Args[1:]), true
		case "list":
			return m.listPlugins(), true
		case "enable":
			return m.enablePlugin(ctx, cmd.Args[1:], true), true
		case "disable":
			return m.enablePlugin(ctx, cmd.Args[1:], false), true
		case "info":
			return m.pluginInfo(cmd.Args[1:]), true
		default:
			return fmt.Sprintf("Unknown subcommand: %s\n%s", cmd.Args[0], plugin.Usage()), true
		}
	})
	p.RegisterMenu("plugin", m.pluginMenu)
}

func (m *Manager) pluginMenu(_ context.Context, cmd *command.Command) (protocol.CommandMenu, bool) {
	if len(cmd.Args) == 0 {
		return protocol.CommandMenu{Title: "Plugin action", Items: []protocol.CommandMenuItem{
			{Value: "/plugin install", Label: "Install", Description: "Install from a local path or URL"},
			{Value: "/plugin enable", Label: "Enable", Description: "Activate an installed plugin"},
			{Value: "/plugin disable", Label: "Disable", Description: "Deactivate an installed plugin"},
			{Value: "/plugin info", Label: "Info", Description: "Inspect an installed plugin"},
			{Value: "/plugin uninstall", Label: "Uninstall", Description: "Remove an installed plugin"},
			{Value: "/plugin list", Label: "List", Description: "Show installed plugins", Submit: true},
		}}, true
	}
	if len(cmd.Args) != 1 {
		return protocol.CommandMenu{}, false
	}
	action := cmd.Args[0]
	if action == "install" || action == "list" {
		return protocol.CommandMenu{}, false
	}
	if action != "enable" && action != "disable" && action != "info" && action != "uninstall" {
		return protocol.CommandMenu{}, false
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	plugins := m.plugins.ListPlugins()
	items := make([]protocol.CommandMenuItem, 0, len(plugins))
	for _, item := range plugins {
		failed := m.agentErrors[item.Name] != ""
		if action == "enable" && item.Enabled && !failed || action == "disable" && !item.Enabled {
			continue
		}
		state := "disabled"
		if item.Enabled {
			state = "enabled"
		}
		if failed {
			state = "activation failed; retry available"
		}
		items = append(items, protocol.CommandMenuItem{
			Value: "/plugin " + action + " " + item.Name,
			Label: item.Name, Description: state, Submit: true,
		})
	}
	return protocol.CommandMenu{
		Title: "Choose plugin", Empty: "No matching plugins", Items: items,
	}, true
}

func (m *Manager) installPlugin(ctx context.Context, args []string, confirm InstallConfirm) string {
	if len(args) == 0 {
		return plugin.InstallUsage
	}
	source := args[0]
	confirmed := len(args) >= 2 && args[1] == "--yes"
	if confirmed {
		return m.install(ctx, source)
	}

	if !plugin.IsLocalSource(source) {
		return m.fetchAndConfirmRemote(ctx, source, confirm)
	}

	p, remote, err := m.plugins.Preview(ctx, source)
	if err != nil {
		return fmt.Sprintf("Preview failed: %v", err)
	}
	if confirm == nil || !confirm(source, p, remote) {
		return "Install cancelled: " + source
	}
	return m.install(ctx, source)
}

func (m *Manager) fetchAndConfirmRemote(ctx context.Context, source string, confirm InstallConfirm) string {
	p, remote, err := m.plugins.Preview(ctx, source)
	if err != nil {
		return fmt.Sprintf("%v\n\n/plugin install %s --yes to skip preview.", err, source)
	}
	if confirm == nil || !confirm(source, p, remote) {
		return "Install cancelled: " + source
	}
	return m.install(ctx, source)
}

func (m *Manager) uninstallPlugin(ctx context.Context, args []string) string {
	if len(args) == 0 {
		return "Usage: /plugin uninstall <name>"
	}
	if err := ctx.Err(); err != nil {
		return "Uninstall cancelled: " + err.Error()
	}

	m.ops.Lock()
	defer m.ops.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	name := strings.Join(args, " ")
	p, ok := m.plugins.Get(name)
	if !ok {
		return fmt.Sprintf("Uninstall failed: plugin %q not found", name)
	}
	if p.Enabled {
		m.deactivateLocked(p.Name)
	}
	if _, err := m.plugins.Remove(name); err != nil {
		if p.Enabled {
			_ = m.activateLocked(context.Background(), p)
		}
		return fmt.Sprintf("Uninstall failed: %v", err)
	}
	delete(m.agentErrors, name)
	m.skills.Reload(m.activeSkillDirsLocked())
	m.syncSkillCommandsLocked()
	return fmt.Sprintf("Uninstalled plugin %q.", name)
}

func (m *Manager) install(ctx context.Context, source string) string {
	m.ops.Lock()
	defer m.ops.Unlock()
	installation, err := m.plugins.PrepareInstall(ctx, source)
	if err != nil {
		return fmt.Sprintf("Install failed: %v", err)
	}
	p := installation.Plugin()
	if err := ctx.Err(); err != nil {
		_ = installation.Rollback()
		return "Install cancelled: " + err.Error()
	}

	m.mu.Lock()
	previous, wasActive := m.active[p.Name]
	previousError := m.agentErrors[p.Name]
	restore := func() {
		delete(m.agentErrors, p.Name)
		if wasActive {
			_ = m.activateLocked(context.Background(), previous.plugin)
		} else if previousError != "" {
			m.agentErrors[p.Name] = previousError
		}
	}
	if wasActive {
		m.deactivateLocked(p.Name)
	}
	if p.Enabled {
		if err := m.activateLocked(ctx, p); err != nil {
			rollbackErr := installation.Rollback()
			restore()
			m.mu.Unlock()
			if rollbackErr != nil {
				return fmt.Sprintf("Install cancelled: %v (rollback failed: %v)", err, rollbackErr)
			}
			return "Install cancelled: " + err.Error()
		}
	}
	if err := installation.Commit(); err != nil {
		m.deactivateLocked(p.Name)
		restore()
		m.mu.Unlock()
		return "Install failed: " + err.Error()
	}
	m.skills.Reload(m.activeSkillDirsLocked())
	m.syncSkillCommandsLocked()
	m.mu.Unlock()

	return plugin.InstallResult(p)
}

func (m *Manager) listPlugins() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	text := m.plugins.ListText()
	for _, p := range m.plugins.ListPlugins() {
		if err := m.agentErrors[p.Name]; err != "" {
			text += "\n" + p.Name + ": " + err
		}
	}
	return text
}

func (m *Manager) pluginInfo(args []string) string {
	if len(args) == 0 {
		return "Usage: /plugin info <name>"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	name := strings.Join(args, " ")
	info, ok := m.plugins.InfoText(name)
	if !ok {
		return fmt.Sprintf("Plugin %q not found.", name)
	}
	if err := m.agentErrors[name]; err != "" {
		info += "\nAgent load error: " + err
	}
	for _, agent := range m.agentInfosLocked() {
		if agent.Plugin == name {
			info += fmt.Sprintf("\nAgent: %s — %s (tools: %s)", agent.ID, agent.Description, strings.Join(agent.Tools, ", "))
		}
	}
	return info
}

func (m *Manager) enablePlugin(ctx context.Context, args []string, enabled bool) string {
	if len(args) == 0 {
		if enabled {
			return "Usage: /plugin enable <name>"
		}
		return "Usage: /plugin disable <name>"
	}
	name := strings.Join(args, " ")
	changed, err := m.setPluginEnabled(ctx, name, enabled)
	if err != nil {
		action := "Enable"
		if !enabled {
			action = "Disable"
		}
		return fmt.Sprintf("%s failed: %v", action, err)
	}
	if !changed {
		state := "disabled"
		if enabled {
			state = "enabled"
		}
		return fmt.Sprintf("Plugin %q is already %s.", name, state)
	}
	if enabled {
		return fmt.Sprintf("Enabled plugin %q.", name)
	}
	return fmt.Sprintf("Disabled plugin %q.", name)
}
