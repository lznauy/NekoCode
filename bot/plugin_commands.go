package bot

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"nekocode/bot/command"
	"nekocode/bot/debug"
	"nekocode/bot/plugin"
	"nekocode/bot/skill"
	"nekocode/bot/tools"
	"nekocode/common"
)

func (b *Bot) registerPluginCommands() {
	b.cmdParser.Register("plugin", func(cmd *command.Command) (string, bool) {
		if len(cmd.Args) == 0 {
			return b.pluginUsage(), true
		}
		switch cmd.Args[0] {
		case "install":
			return b.pluginInstall(cmd.Args[1:])
		case "uninstall":
			return b.pluginUninstall(cmd.Args[1:])
		case "list":
			return b.pluginList(cmd.Args[1:])
		case "enable":
			return b.pluginEnable(cmd.Args[1:])
		case "disable":
			return b.pluginDisable(cmd.Args[1:])
		case "info":
			return b.pluginInfo(cmd.Args[1:])
		default:
			return fmt.Sprintf("Unknown subcommand: %s\n%s", cmd.Args[0], b.pluginUsage()), true
		}
	})
}

func (b *Bot) pluginUsage() string {
	return "Usage: /plugin <subcommand> [args]\n\nSubcommands:\n  install <source>   Install from GitHub URL, user/repo, or local path\n  uninstall <name>   Remove a plugin\n  list               List installed plugins\n  enable <name>      Enable a disabled plugin\n  disable <name>     Disable a plugin (keeps files)\n  info <name>        Show plugin details"
}

func (b *Bot) pluginInstall(args []string) (string, bool) {
	if len(args) == 0 {
		return "Usage: /plugin install <source>\n  source: GitHub URL | user/repo | ./local-path", true
	}
	source := args[0]
	confirmed := len(args) >= 2 && args[1] == "--yes"

	// Local path: read manifest (fast), show confirm bar.
	if isLocalPath(source) {
		p, err := b.pluginReg.PreviewFromPath(source)
		if err != nil {
			return fmt.Sprintf("Preview failed: %v", err), true
		}
		if !confirmed {
			b.confirmMu.Lock()
			b.pendingConfirm = true
			b.confirmMu.Unlock()
			go func() {
				if b.confirmInstall(source, p, false) {
					result, _ := b.pluginInstallSync(source)
					if b.notifyFn != nil {
						b.notifyFn(result)
					}
				}
			}()
			return b.formatInstallPreview(p), true
		}
		return b.pluginInstallSync(source)
	}

	// Remote: fetch manifest first, then show confirm bar.
	if !confirmed {
		b.confirmMu.Lock()
		b.pendingConfirm = true
		b.confirmMu.Unlock()
		go b.fetchAndConfirmRemote(source)
		return fmt.Sprintf("Fetching plugin info from %s ...", source), true
	}

	go b.pluginInstallAsync(source)
	return fmt.Sprintf("Installing from %s ...", source), true
}

func (b *Bot) fetchAndConfirmRemote(source string) {
	data, err := fetchURL(sourceToRawURL(source))
	if err != nil {
		if b.notifyFn != nil {
			b.notifyFn(fmt.Sprintf("Failed to fetch plugin info: %v\n\n/plugin install %s --yes  to skip preview.", err, source))
		}
		b.unblockConfirm()
		return
	}
	m, err := plugin.ParseManifestData(data)
	if err != nil {
		if b.notifyFn != nil {
			b.notifyFn(fmt.Sprintf("Invalid plugin.json: %v", err))
		}
		b.unblockConfirm()
		return
	}
	p := &plugin.Plugin{Manifest: *m, Dir: "", Source: source}
	if b.confirmInstall(source, p, true) {
		b.pluginInstallAsync(source)
	}
}

func (b *Bot) unblockConfirm() {
	b.confirmMu.Lock()
	b.pendingConfirm = false
	b.confirmMu.Unlock()
	if b.confirmCh != nil {
		select {
		case b.confirmCh <- common.ConfirmRequest{Response: nil}:
		default:
		}
	}
}

func (b *Bot) confirmInstall(source string, p *plugin.Plugin, isRemote bool) bool {
	summary := b.formatInstallPreview(p)
	if isRemote {
		summary += "\n(install.sh will not be executed automatically)"
	}
	if b.confirmFn == nil {
		b.unblockConfirm()
		return false
	}
	result := b.confirmFn(common.NewConfirmRequest("/plugin install", map[string]any{"source": source, "summary": summary}, common.LevelWrite))
	b.confirmMu.Lock()
	b.pendingConfirm = false
	b.confirmMu.Unlock()
	if !result {
		if b.notifyFn != nil {
			b.notifyFn(fmt.Sprintf("Install cancelled: %s", source))
		}
	}
	return result
}

func sourceToRawURL(source string) string {
	s := source
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	if !strings.HasPrefix(s, "github.com/") && !strings.HasPrefix(s, "raw.githubusercontent.com/") {
		return ""
	}
	if strings.HasPrefix(s, "github.com/") {
		clean := strings.TrimSuffix(strings.TrimSuffix(s, ".git"), "/")
		// github.com/user/repo/tree/<branch> or github.com/user/repo
		parts := strings.SplitN(clean, "/", 6)
		if len(parts) < 3 {
			return ""
		}
		branch := "main"
		if len(parts) >= 5 && parts[3] == "tree" {
			branch = parts[4]
		}
		return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/.claude-plugin/plugin.json", parts[1], parts[2], branch)
	}
	// Already raw URL, add plugin.json path if needed.
	if !strings.Contains(s, ".claude-plugin") {
		s = strings.TrimSuffix(s, "/") + "/.claude-plugin/plugin.json"
	}
	return "https://" + s
}

func (b *Bot) formatInstallPreview(p *plugin.Plugin) string {
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

func isLocalPath(s string) bool {
	return strings.HasPrefix(s, "./") || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~") ||
		(!strings.Contains(s, "://") && !common.LooksLikeGit(s))
}

func fetchURL(url string) ([]byte, error) {
	ctx, cancel := common.ShortContext()
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := tools.NewToolHTTPClient(10 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64*1024))
}

func (b *Bot) pluginInstallSync(source string) (string, bool) {
	p, err := b.pluginReg.Install(source)
	if err != nil {
		return fmt.Sprintf("Install failed: %v", err), true
	}
	return b.registerPluginExtensions(p)
}

func (b *Bot) pluginInstallAsync(source string) {
	p, err := b.pluginReg.Install(source)
	if err != nil {
		if b.notifyFn != nil {
			b.notifyFn(fmt.Sprintf("Install failed: %v", err))
		}
		return
	}
	result, _ := b.registerPluginExtensions(p)
	if b.notifyFn != nil {
		b.notifyFn(result)
	}
}

func (b *Bot) registerPluginExtensions(p *plugin.Plugin) (string, bool) {
	for _, d := range p.SkillDirs() {
		if err := b.skillReg.Load([]string{d}); err != nil {
			debug.Log("plugin: skill load error: %v", err)
		}
	}
	b.refreshSkillList()

	b.loadPluginExtensions(p)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Installed plugin %q v%s.\n", p.Name, p.Version)
	if p.HasInstallStub {
		sb.WriteString("Note: This plugin has an install.sh script. Run it manually if needed:\n")
		fmt.Fprintf(&sb, "  cd %s && bash install.sh\n", p.Dir)
	}
	agentCount := len(p.AgentPaths())
	fmt.Fprintf(&sb, "Skills: %d  Agents: %d  MCP: %d", len(p.SkillDirs()), agentCount, len(p.MCPServers()))
	return sb.String(), true
}

func (b *Bot) pluginUninstall(args []string) (string, bool) {
	if len(args) == 0 {
		return "Usage: /plugin uninstall <name>", true
	}
	name := args[0]
	if p, ok := b.pluginReg.Get(name); ok {
		b.unloadPluginExtensions(p)
	}
	if err := b.pluginReg.Uninstall(name); err != nil {
		return fmt.Sprintf("Uninstall failed: %v", err), true
	}
	b.refreshPluginSkills()
	return fmt.Sprintf("Uninstalled plugin %q.", name), true
}

func (b *Bot) pluginList(args []string) (string, bool) {
	plugins := b.pluginReg.List()
	if len(plugins) == 0 {
		return "No plugins installed. Use /plugin install <source> to add one.", true
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
	return sb.String(), true
}

func (b *Bot) pluginEnable(args []string) (string, bool) {
	p, ok := b.requirePluginArg(args, "enable")
	if !ok {
		if len(args) == 0 {
			return "Usage: /plugin enable <name>", true
		}
		return fmt.Sprintf("Plugin %q not found.", args[0]), true
	}
	if p.Enabled {
		return fmt.Sprintf("Plugin %q is already enabled.", p.Name), true
	}
	if err := b.pluginReg.Enable(p.Name); err != nil {
		return fmt.Sprintf("Enable failed: %v", err), true
	}
	b.loadPluginExtensions(p)
	b.refreshPluginSkills()
	return fmt.Sprintf("Enabled plugin %q.", p.Name), true
}

func (b *Bot) pluginDisable(args []string) (string, bool) {
	p, ok := b.requirePluginArg(args, "disable")
	if !ok {
		if len(args) == 0 {
			return "Usage: /plugin disable <name>", true
		}
		return fmt.Sprintf("Plugin %q not found.", args[0]), true
	}
	if !p.Enabled {
		return fmt.Sprintf("Plugin %q is already disabled.", p.Name), true
	}
	b.unloadPluginExtensions(p)
	if err := b.pluginReg.Disable(p.Name); err != nil {
		return fmt.Sprintf("Disable failed: %v", err), true
	}
	b.refreshPluginSkills()
	return fmt.Sprintf("Disabled plugin %q.", p.Name), true
}

func (b *Bot) pluginInfo(args []string) (string, bool) {
	p, ok := b.requirePluginArg(args, "info")
	if !ok {
		if len(args) == 0 {
			return "Usage: /plugin info <name>", true
		}
		return fmt.Sprintf("Plugin %q not found.", args[0]), true
	}
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
	return sb.String(), true
}

func (b *Bot) refreshPluginSkills() {
	b.reloadSkills(b.skillReg.LoadedSet())
}

// refreshSkillList rebuilds the skill list text in the context manager.
func (b *Bot) refreshSkillList() {
	b.ctxMgr.SetSkillList(skill.BuildSkillListText(b.skillReg.List(), b.skillReg.LoadedSet(), b.cfg.ContextWindow))
}

// requirePluginArg validates that args is non-empty and the named plugin exists.
// Returns the plugin and true on success, or an error message and false on failure.
func (b *Bot) requirePluginArg(args []string, subcmd string) (*plugin.Plugin, bool) {
	if len(args) == 0 {
		return nil, false
	}
	p, ok := b.pluginReg.Get(args[0])
	if !ok {
		return nil, false
	}
	return p, true
}
