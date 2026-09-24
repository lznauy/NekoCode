package extension

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"nekocode/bot/command"
	"nekocode/bot/extension/mcp"
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
	for _, action := range []string{"login", "logout", "cancel"} {
		// Hidden: the /mcp menu drives these on the user's behalf (login is
		// its only entry point in interactive UIs), but they stay executable
		// for text transports without a picker.
		p.RegisterHiddenInfo("mcp-"+action, "MCP OAuth "+action, func(_ context.Context, cmd *command.Command) (string, bool) {
			if len(cmd.Args) != 1 {
				return "Usage: /mcp-" + action + " <server>", true
			}
			name := cmd.Args[0]
			if err := m.MCPAuthorizationAction(name, action); err != nil {
				return err.Error(), true
			}
			if action != "login" {
				return "MCP " + action + " done. Use /mcp to check status.", true
			}
			return name + " 授权已启动，链接生成后将通知；也可用 /mcp 查看或取消。", true
		})
	}
	// /mcp: read-only status query, so it stays available during a task. The
	// menu surfaces per-server state; ready servers are green and inert,
	// unauthorized ones launch the login flow directly.
	p.RegisterLocalInfo("mcp", "Show connected MCP servers", func(_ context.Context, _ *command.Command) (string, bool) {
		return m.mcpStatus(), true
	})
	p.RegisterMenu("mcp", m.mcpMenu)
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

func (m *Manager) mcpMenu(_ context.Context, cmd *command.Command) (protocol.CommandMenu, bool) {
	health, definitions, names := m.mcpInventory()
	if len(names) == 0 {
		return protocol.CommandMenu{Title: "MCP 服务器", Empty: "未连接任何 MCP 服务器"}, true
	}
	items := make([]protocol.CommandMenuItem, 0, len(names))
	for _, name := range names {
		h, running := health[name]
		definition, configured := definitions[name]
		source, sourceDetail := mcpDisplaySource(definition, configured, h)
		description := sourceDetail
		// Logout only makes sense for servers that actually hold a stored
		// OAuth credential; stdio and never-authorized remotes omit the row.
		logout := m.mcp.HasStoredCredential(name)
		if configured && !definition.Enabled {
			items = append(items, protocol.CommandMenuItem{
				Label: name + " · 已禁用 · " + source, Description: description,
				Value: "/mcp", Current: true,
			})
			continue
		}
		if !running {
			items = append(items, protocol.CommandMenuItem{
				Label: name + " · 未加载 · " + source, Description: description,
				Value: "/mcp", Current: true,
			})
			continue
		}
		switch h.Status {
		case mcp.StatusReady:
			// Green and inert: nothing to do for an authorized server.
			items = append(items, protocol.CommandMenuItem{
				Label:       fmt.Sprintf("%s · 已就绪（%d 个工具） · %s", name, h.ToolCount, source),
				Description: description, Value: "/mcp", Current: true,
			})
			if logout {
				items = append(items, logoutItem(name))
			}
		case mcp.StatusAuthRequired:
			items = append(items, protocol.CommandMenuItem{
				Label: name + " · 需要授权 · " + source, Description: "回车获取授权链接 · " + description,
				Value: "/mcp-login " + name, Submit: true,
			})
			if logout {
				items = append(items, logoutItem(name))
			}
		case mcp.StatusAuthorizing:
			items = append(items, protocol.CommandMenuItem{
				Label: name + " · 等待浏览器授权 · " + source, Description: "回车重新显示授权链接 · " + description,
				Value: "/mcp-login " + name, Submit: true,
			})
			items = append(items, protocol.CommandMenuItem{
				Label: name + " · 取消授权", Description: "放弃等待中的浏览器授权",
				Value: "/mcp-cancel " + name, Submit: true,
			})
		case mcp.StatusError:
			items = append(items, protocol.CommandMenuItem{
				Label: name + " · 连接失败 · " + source, Description: strings.TrimSpace(h.Error + " · " + description),
				Value: "/mcp-login " + name, Submit: true,
			})
			if logout {
				items = append(items, logoutItem(name))
			}
		default:
			items = append(items, protocol.CommandMenuItem{
				Label: name + " · 连接中 · " + source, Description: description, Value: "/mcp", Current: true,
			})
		}
	}
	return protocol.CommandMenu{Title: "MCP 服务器", Empty: "未连接任何 MCP 服务器", Items: items}, true
}

// logoutItem offers credential deletion for a connected server.
func logoutItem(name string) protocol.CommandMenuItem {
	return protocol.CommandMenuItem{
		Label: name + " · 退出登录", Description: "删除本机保存的授权凭据",
		Value: "/mcp-logout " + name, Submit: true,
	}
}

// mcpStatus merges user-visible host definitions with runtime health. This
// keeps disabled or failed-to-register workspace entries visible; plugin and
// session servers are described by their runtime owner label.
func (m *Manager) mcpStatus() string {
	health, definitions, names := m.mcpInventory()
	if len(names) == 0 {
		return "未连接任何 MCP 服务器。"
	}
	ready := 0
	var b strings.Builder
	for _, name := range names {
		h := health[name]
		if h.Status == mcp.StatusReady {
			ready++
		}
		definition, configured := definitions[name]
		fmt.Fprintf(&b, "  %-20s %s\n", name, strings.Join(mcpStatusParts(name, h, definition, configured), " · "))
		if h.AuthURL != "" {
			fmt.Fprintf(&b, "    授权链接：%s\n", h.AuthURL)
		}
	}
	return fmt.Sprintf("MCP 服务器 · %d/%d 已就绪\n%s", ready, len(names), strings.TrimRight(b.String(), "\n"))
}

func (m *Manager) mcpInventory() (map[string]mcp.Health, map[string]ConfiguredMCP, []string) {
	m.mu.Lock()
	definitions := make(map[string]ConfiguredMCP, len(m.configuredMCP)+len(m.configMCP))
	for _, definition := range m.configuredMCP {
		definitions[definition.Name] = definition
	}
	for name, cfg := range m.configMCP {
		if _, exists := definitions[name]; !exists {
			definitions[name] = ConfiguredMCP{Name: name, Source: "配置", URL: cfg.URL, Command: cfg.Command, Args: append([]string(nil), cfg.Args...), Enabled: true}
		}
	}
	m.mu.Unlock()
	health := m.mcp.Health()
	namesByValue := make(map[string]struct{}, len(health)+len(definitions))
	for name := range health {
		namesByValue[name] = struct{}{}
	}
	for name := range definitions {
		namesByValue[name] = struct{}{}
	}
	names := make([]string, 0, len(namesByValue))
	for name := range namesByValue {
		names = append(names, name)
	}
	sort.Strings(names)
	return health, definitions, names
}

func mcpStatusParts(name string, h mcp.Health, definition ConfiguredMCP, configured bool) []string {
	status := h.Status
	if status == "" {
		status = "未加载"
		if configured && !definition.Enabled {
			status = "已禁用"
		}
	}
	parts := []string{status}
	switch {
	case h.Status == mcp.StatusReady:
		parts = append(parts, fmt.Sprintf("%d tools", h.ToolCount))
	case h.Status == mcp.StatusAuthRequired:
		// The SDK error behind this state is noise; the actionable hint is
		// the command. Once a login flow starts, the URL line appears above.
		parts = append(parts, fmt.Sprintf("运行 /mcp-login %s 完成授权", name))
	case h.Error != "":
		parts = append(parts, h.Error)
	}
	source, detail := mcpDisplaySource(definition, configured, h)
	parts = append(parts, source)
	if detail != "" && detail != source {
		parts = append(parts, detail)
	}
	if definition.Command != "" {
		parts = append(parts, strings.TrimSpace(definition.Command+" "+strings.Join(definition.Args, " ")))
	}
	return parts
}

func mcpDisplaySource(definition ConfiguredMCP, configured bool, h mcp.Health) (string, string) {
	if !configured || definition.Source == "" {
		source := mcpOwnerLabel(h.Owner)
		return source, source
	}
	if definition.Source == "配置" {
		return "配置", "配置"
	}
	return "工作区", definition.Source
}

// mcpOwnerLabel turns a lifecycle owner ID into a short source label.
func mcpOwnerLabel(owner string) string {
	prefix, rest, found := strings.Cut(owner, ":")
	if !found {
		return owner
	}
	switch prefix {
	case "config":
		return "配置"
	case "plugin", "session":
		name, _, _ := strings.Cut(rest, ":")
		if name == "" {
			return prefix
		}
		label := "插件"
		if prefix == "session" {
			label = "会话"
		}
		return label + " " + name
	default:
		return owner
	}
}
