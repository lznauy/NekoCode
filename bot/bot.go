package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"nekocode/bot/agent"
	"nekocode/bot/agent/subagent"
	"nekocode/bot/command"
	"nekocode/bot/config"
	"nekocode/bot/ctxmgr"
	"nekocode/bot/ctxmgr/compact"
	"nekocode/bot/ctxmgr/memory"
	"nekocode/bot/debug"
	"nekocode/bot/hooks"
	"nekocode/bot/mcp"
	"nekocode/bot/plugin"
	"nekocode/bot/cindex"
	"nekocode/bot/prompt"
	"nekocode/bot/session"
	"nekocode/bot/skill"
	"nekocode/bot/skill/bundled"
	"nekocode/bot/tools"
	"nekocode/bot/tools/builtin"
	"nekocode/common"
	"nekocode/llm"
	"nekocode/llm/types"
)

type Bot struct {
	cfg            *config.Config
	ctxMgr         *ctxmgr.Manager
	cmdParser      *command.Parser
	skillState     *command.SkillState
	ag             *agent.Agent
	sess           *session.Snapshot
	skillReg       *skill.Registry
	pluginReg      *plugin.Registry
	mcpClients     map[string]*mcp.Client
	confirmFn      common.ConfirmFunc
	phaseFn        common.PhaseFunc
	todoFn         common.TodoFunc
	notifyFn       func(string)
	confirmCh      chan common.ConfirmRequest
	confirmMu      sync.Mutex
	pendingConfirm bool
	promptBuilder  *prompt.Builder
	toolRegistry   *tools.Registry
	hookReg        *hooks.Registry
	projCtx        string // cached project context for model switching
	cindexMgr      *cindex.Manager
	mu             sync.Mutex
}

func New() *Bot {
	b := &Bot{}

	b.initConfig()
	b.initCtxMgr()
	b.initToolRegistry()

	b.initHooks()
	b.initSummarizer()
	b.initPlugins()
	b.initSkills()
	b.initSession()
	b.initAgent()
	b.initCommands()

	return b
}

// -- init blocks ----------------------------------------------------------

func (b *Bot) initConfig() {
	b.cfg, _ = config.Load()
	cwd, _ := os.Getwd()
	b.promptBuilder = prompt.NewBuilder(cwd)
}

func (b *Bot) initCtxMgr() {
	systemPrompt := b.promptBuilder.Build()
	memFile, _ := memory.Load(memory.DefaultPath())
	b.ctxMgr = ctxmgr.New(ctxmgr.Config{SystemPrompt: systemPrompt, Memory: memFile})

	cwd, err := os.Getwd()
	if err != nil {
		b.ctxMgr.SetContextWindow(b.cfg.ContextWindow)
		return
	}

	b.projCtx = cindex.LoadProjectContext(cwd)
	if b.projCtx != "" {
		b.ctxMgr.Add("system", b.projCtx)
	}

	mgr, err := cindex.NewManager(cwd)
	if err != nil {
		b.ctxMgr.SetContextWindow(b.cfg.ContextWindow)
		return
	}
	if err := mgr.Init(); err != nil {
		b.ctxMgr.SetContextWindow(b.cfg.ContextWindow)
		return
	}

	if g := mgr.Graph(); g != nil {
		if skeleton := g.FormatSkeleton(cwd); skeleton != "" {
			b.ctxMgr.Add("system", skeleton)
		}
	}

	b.cindexMgr = mgr
	b.ctxMgr.SetContextWindow(b.cfg.ContextWindow)
}

func (b *Bot) initSummarizer() {
	b.ctxMgr.CM.Summarizer = func(msgs []types.Message, prevSummary string) (string, error) {
		prompt := compact.BuildPrompt(msgs, prevSummary)
		resp, err := b.ctxMgr.MergeClient.Chat(context.Background(), []types.Message{{Role: "user", Content: prompt}}, nil)
		if err != nil {
			return "", err
		}
		if len(resp.Choices) > 0 {
			return resp.Choices[0].Message.Content, nil
		}
		return "", nil
	}
}

func (b *Bot) initToolRegistry() {
	b.toolRegistry = tools.NewRegistry()
	builtin.RegisterAll(b.toolRegistry, b.cfg.ImageGenModels)

	if b.cindexMgr != nil {
		b.toolRegistry.Register(cindex.NewProjectInfoTool(b.cindexMgr))
	}

	tools.GlobalFileCache = tools.NewFileStateCache()
}

func (b *Bot) initSkills() {
	b.skillReg = skill.NewRegistry()
	b.skillReg.RegisterBundled(bundled.All())
	dirs := skill.DefaultDirs()
	for _, d := range b.pluginReg.SkillDirs() {
		dirs = append(dirs, d)
	}
	if err := b.skillReg.Load(dirs); err != nil {
		fmt.Fprintf(os.Stderr, "skill: load error: %v\n", err)
	}
	b.toolRegistry.Register(skill.NewSkillTool(b.skillReg))
	b.ctxMgr.SetSkillList(skill.BuildSkillListText(b.skillReg.List(), nil, b.cfg.ContextWindow))
}

func (b *Bot) initPlugins() {
	b.pluginReg = plugin.NewRegistry(plugin.DefaultDirs())
	b.pluginReg.Logf = debug.Log
	b.mcpClients = make(map[string]*mcp.Client)

	b.pluginReg.LoadAll()

	for _, p := range b.pluginReg.List() {
		if !p.Enabled {
			continue
		}
		b.loadPluginExtensions(p)
	}
}

// loadPluginExtensions registers agents, hooks, and MCP tools from a plugin.
// Used during initial load, post-install, and enable.
func (b *Bot) loadPluginExtensions(p *plugin.Plugin) {
	for _, agentPath := range p.AgentPaths() {
		def, err := subagent.ParseAgentMD(agentPath)
		if err != nil {
			debug.Log("plugin: agent %s: %v", agentPath, err)
			continue
		}
		subagent.RegisterPlugin(def.ToAgentType())
	}
	if hooksPath, ok := p.HooksPath(); ok {
		if pluginHooks, err := hooks.LoadPluginHooks(p.Dir, hooksPath); err == nil {
			for _, h := range pluginHooks {
				b.hookReg.Register(h)
			}
		} else {
			debug.Log("plugin: hooks %s: %v", hooksPath, err)
		}
	}
	for name, cfg := range p.MCPServers() {
		level := mcp.ParseDangerLevel(cfg.DangerLevel)
		client := mcp.NewClient(name, mcp.ServerConfig{
			Command:     cfg.Command,
			Args:        cfg.Args,
			Env:         expandMCPEnv(cfg.Env, p.Dir),
			DangerLevel: cfg.DangerLevel,
		})
		// Close old client if name collides (two plugins with same MCP server name).
		if old, exists := b.mcpClients[name]; exists {
			old.Close()
		}
		b.mcpClients[name] = client

		if err := client.Start(); err != nil {
			debug.Log("plugin: mcp %s start: %v", name, err)
			continue
		}
		mcpTools, err := client.ListTools()
		if err != nil {
			debug.Log("plugin: mcp %s list tools: %v", name, err)
			continue
		}
		for _, td := range mcpTools {
			b.toolRegistry.Register(mcp.NewMCPTool(client, td, level))
		}
	}
}

// unloadPluginExtensions unregisters agents, hooks, and MCP tools from a plugin.
// Used during uninstall and disable.
func (b *Bot) unloadPluginExtensions(p *plugin.Plugin) {
	for _, ap := range p.AgentPaths() {
		def, err := subagent.ParseAgentMD(ap)
		if err == nil {
			subagent.UnregisterPlugin(def.Name)
		}
	}
	for srvName := range p.MCPServers() {
		if client, ok := b.mcpClients[srvName]; ok {
			for _, t := range b.toolRegistry.List() {
				if strings.HasPrefix(t.Name(), client.Name+"__") {
					b.toolRegistry.Unregister(t.Name())
				}
			}
			client.Close()
			delete(b.mcpClients, srvName)
		}
	}
	for _, h := range b.hookReg.List() {
		if strings.HasPrefix(h.Name, "plugin:") && strings.Contains(h.Name, p.Dir) {
			b.hookReg.Unregister(h.Name)
		}
	}
}

func expandMCPEnv(env map[string]string, pluginRoot string) map[string]string {
	if env == nil {
		return nil
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		s := strings.ReplaceAll(v, "${CLAUDE_PLUGIN_ROOT}", pluginRoot)
		s = strings.ReplaceAll(s, "${PLUGIN_ROOT}", pluginRoot)
		out[k] = s
	}
	return out
}

func fetchURL(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64*1024))
}

func (b *Bot) initSession() {
	b.cmdParser = command.NewParser()
	b.skillState = &command.SkillState{MsgStart: -1}

	cwd, _ := os.Getwd()
	sess, err := session.New(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "session: %v — running without session persistence\n", err)
		return
	}
	b.sess = sess
}

func (b *Bot) initAgent() {
	am := b.cfg.ActiveModelConfig()
	llmClient := llm.NewClientWithProtocol(am.Provider, am.APIKey, am.BaseURL, am.Model, am.Protocol)

	fm := b.cfg.ResolveModel(b.cfg.FlashModel)
	mergeClient := llm.NewClientWithProtocol(fm.Provider, fm.APIKey, fm.BaseURL, fm.Model, fm.Protocol)
	mergeClient.SetDisableThinking(true)
	mergeClient.SetMaxTokens(2000)
	b.ctxMgr.MergeClient = mergeClient

	b.ag = agent.New(context.Background(), b.ctxMgr, llmClient, b.toolRegistry)
	b.ag.SetHookRegistry(b.hookReg)

	// Restore callbacks from Configure (if already called).
	if b.confirmFn != nil {
		b.ag.SetConfirmFn(b.confirmFn)
	}
	if b.phaseFn != nil {
		b.ag.SetPhaseFn(b.phaseFn)
	}
	if b.todoFn != nil {
		b.ag.WireTodoWrite(func(items []common.TodoItem) {
			b.ctxMgr.SetTodos(items)
			b.todoFn(items)
		})
	}

	// Restore guardrails context transform.
	b.ag.SetContextTransform(func(msgs []types.Message) []types.Message {
		toolResults := 0
		for _, m := range msgs {
			if m.Role == "tool" {
				toolResults++
			}
		}
		if toolResults > 40 {
			msgs = append(msgs, types.Message{
				Role:    "user",
				Content: "[System] " + strconv.Itoa(toolResults) + " tool results accumulated. Check for unfinished sub-tasks — if any, continue with task. If all done, call task(verify) to validate, then report results.",
			})
		}
		return msgs
	})

	subLLM := llm.NewClientWithProtocol(fm.Provider, fm.APIKey, fm.BaseURL, fm.Model, fm.Protocol)
	engine := subagent.NewEngine(subLLM, b.toolRegistry, b.ctxMgr.MergeClient)

	if t, err := b.toolRegistry.Get("task"); err == nil {
		cwd, _ := os.Getwd()
		t.(*builtin.TaskTool).Wire(func(ctx context.Context, prompt, agentType, thoroughness string) (*subagent.Result, error) {
			at, ok := subagent.Get(agentType)
			if !ok {
				return nil, fmt.Errorf("unknown sub-agent type: %s", agentType)
			}
			cfg := subagent.RunConfig{
				Prompt:          prompt,
				AgentType:       at,
				Cwd:             cwd,
				ProjectContext:  b.projCtx,
				Thoroughness:    thoroughness,
				ContextWindow:   b.cfg.ContextWindow,
				DisableThinking: true,
				ConfirmFn:       b.ag.ConfirmFn(),
			}
			if fn := b.ag.PhaseFn(); fn != nil {
				cfg.OnPhase = func(p string) { fn(at.Name + " · " + p) }
			}
			cfg.AddTokens = b.ag.AddTokens
			result, err := engine.Run(ctx, cfg)
			if result != nil && (result.CacheHitTokens > 0 || result.CacheMissTokens > 0) {
				b.ctxMgr.Tracker.RecordSubagent(result.TotalTokens, result.CacheHitTokens, result.CacheMissTokens)
			}
			return result, err
		})
	}
}

func (b *Bot) initCommands() {
	command.RegisterAll(b.cmdParser, command.Deps{
		CtxMgr:        b.ctxMgr,
		Ag:            b.getAgent,
		SkillReg:      b.skillReg,
		ToolRegistry:  b.toolRegistry,
		ContextWindow: b.cfg.ContextWindow,
		GetConfigFn:   b.ProviderModel,
		ListModelsFn:  b.cfg.AllModelNames,
		FreshStart: func() (string, error) {
			return command.ForceFreshStart(b.ctxMgr, b.skillReg, b.cfg.ContextWindow)
		},
		SwitchModel: b.SwitchModel,
	}, b.skillState)

	b.cmdParser.Register("sessions", func(cmd *command.Command) (string, bool) {
		if len(cmd.Args) > 0 {
			id := cmd.Args[0]
			if err := b.ResumeSession(id); err != nil {
				return fmt.Sprintf("Failed to resume session %s: %v", id, err), true
			}
			return fmt.Sprintf("Resumed session %s (%d messages restored).", id, len(b.sess.Messages)), true
		}
		sessions := session.List()
		if len(sessions) == 0 {
			return "No saved sessions.", true
		}
		var sb strings.Builder
		sb.WriteString("Saved sessions:\n")
		for _, s := range sessions {
			fmt.Fprintf(&sb, "  %s  %s  %d msgs  %s\n", s.ID, s.Age(), s.MsgCount, s.CWD)
		}
		sb.WriteString("\n/sessions <id> to resume")
		return sb.String(), true
	})

	b.cmdParser.Register("export", func(cmd *command.Command) (string, bool) {
		msgs := b.ctxMgr.Build(false)
		data, err := json.MarshalIndent(msgs, "", "  ")
		if err != nil {
			return fmt.Sprintf("Failed to marshal context: %v", err), true
		}
		dir := "/tmp/nekocode"
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Sprintf("Failed to create directory: %v", err), true
		}
		path := filepath.Join(dir, "nekocode-context.json")
		if err := os.WriteFile(path, data, 0644); err != nil {
			return fmt.Sprintf("Failed to write file: %v", err), true
		}
		return fmt.Sprintf("Context exported to %s (%d messages)", path, len(msgs)), true
	})

	b.registerPluginCommands()
}

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
	result := b.confirmFn(common.ConfirmRequest{
		ToolName: "/plugin install",
		Args:     map[string]any{"source": source, "summary": summary},
		Level:    common.LevelWrite,
		Response: make(chan bool, 1),
	})
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
		(!strings.Contains(s, "://") && !looksLikeGit(s))
}

func looksLikeGit(s string) bool {
	parts := strings.Split(s, "/")
	return len(parts) == 2 && !strings.Contains(parts[0], ".") && !strings.Contains(parts[0], ":")
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
	b.ctxMgr.SetSkillList(skill.BuildSkillListText(b.skillReg.List(), b.skillReg.LoadedSet(), b.cfg.ContextWindow))

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
	if len(args) == 0 {
		return "Usage: /plugin enable <name>", true
	}
	name := args[0]
	p, ok := b.pluginReg.Get(name)
	if !ok {
		return fmt.Sprintf("Plugin %q not found.", name), true
	}
	if p.Enabled {
		return fmt.Sprintf("Plugin %q is already enabled.", name), true
	}
	if err := b.pluginReg.Enable(name); err != nil {
		return fmt.Sprintf("Enable failed: %v", err), true
	}
	b.loadPluginExtensions(p)
	b.refreshPluginSkills()
	return fmt.Sprintf("Enabled plugin %q.", name), true
}

func (b *Bot) pluginDisable(args []string) (string, bool) {
	if len(args) == 0 {
		return "Usage: /plugin disable <name>", true
	}
	name := args[0]
	p, ok := b.pluginReg.Get(name)
	if !ok {
		return fmt.Sprintf("Plugin %q not found.", name), true
	}
	if !p.Enabled {
		return fmt.Sprintf("Plugin %q is already disabled.", name), true
	}
	b.unloadPluginExtensions(p)
	if err := b.pluginReg.Disable(name); err != nil {
		return fmt.Sprintf("Disable failed: %v", err), true
	}
	b.refreshPluginSkills()
	return fmt.Sprintf("Disabled plugin %q.", name), true
}

func (b *Bot) pluginInfo(args []string) (string, bool) {
	if len(args) == 0 {
		return "Usage: /plugin info <name>", true
	}
	p, ok := b.pluginReg.Get(args[0])
	if !ok {
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
	b.skillReg = skill.NewRegistry()
	b.skillReg.RegisterBundled(bundled.All())
	dirs := skill.DefaultDirs()
	for _, d := range b.pluginReg.SkillDirs() {
		dirs = append(dirs, d)
	}
	b.skillReg.Load(dirs)
	b.ctxMgr.SetSkillList(skill.BuildSkillListText(b.skillReg.List(), b.skillReg.LoadedSet(), b.cfg.ContextWindow))
	// Re-register SkillTool so it uses the new registry.
	b.toolRegistry.Unregister("skill")
	b.toolRegistry.Register(skill.NewSkillTool(b.skillReg))
}

// -- session persistence --------------------------------------------------

func (b *Bot) saveSession() {
	if b.sess == nil {
		return
	}
	sysPrompt, skills, archive, mem, cb, msgs, budget := b.ctxMgr.Snapshot()
	b.sess.SystemPrompt = sysPrompt
	b.sess.Skills = skills
	b.sess.Memory = mem
	b.sess.Archive = archive
	b.sess.Messages = msgs
	b.sess.CompactBoundary = cb
	b.sess.ContextWindow = budget
	b.mu.Lock()
	b.sess.PromptTokens, b.sess.CompletionTokens = b.ag.TokenUsage()
	b.mu.Unlock()
	for name := range b.skillReg.LoadedSet() {
		b.sess.LoadedSkills = append(b.sess.LoadedSkills, name)
	}
	if err := b.sess.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "session: save error: %v\n", err)
	}
}

func (b *Bot) ResumeSession(id string) error {
	sess, err := session.Load(id)
	if err != nil {
		return fmt.Errorf("session: load: %w", err)
	}
	b.ctxMgr.Restore(sess.SystemPrompt, sess.Skills, sess.Archive, sess.Memory,
		sess.CompactBoundary, sess.Messages, sess.ContextWindow)
	b.mu.Lock()
	b.ag.AddTokens(sess.PromptTokens, sess.CompletionTokens)
	b.mu.Unlock()
	for _, name := range sess.LoadedSkills {
		b.skillReg.MarkLoaded(name)
	}
	b.sess = sess
	return nil
}

func (b *Bot) initHooks() {
	b.hookReg = hooks.NewRegistry()
	hooks.RegisterBuiltin(b.hookReg)
}
