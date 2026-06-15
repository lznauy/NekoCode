package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"nekocode/bot/agent"
	"nekocode/bot/agent/subagent"
	"nekocode/bot/command"
	"nekocode/bot/config"
	"nekocode/bot/cindex"
	"nekocode/bot/ctxmgr"
	"nekocode/bot/ctxmgr/memory"
	"nekocode/bot/debug"
	"nekocode/bot/hooks"
	"nekocode/bot/mcp"
	"nekocode/bot/plugin"
	"nekocode/bot/prompt"
	"nekocode/bot/session"
	"nekocode/bot/skill"
	"nekocode/bot/skill/bundled"
	"nekocode/bot/tools"
	"nekocode/bot/tools/builtin"
	"nekocode/bot/tools/hashline"
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
	cwd                 string // computed once in New()
	lastGuardrailWarned int    // tool result count at last guardrail injection
	sessionResumed      bool   // set by sessions handler, consumed by ExecuteCommand
	mu                  sync.Mutex
}

func New() *Bot {
	b := &Bot{}
	b.cwd, _ = os.Getwd()

	b.initConfig()
	b.initCtxMgr()
	b.initToolRegistry()

	b.initHooks()
	b.initPlugins()
	b.initSkills()
	b.initSession()
	b.initAgent()
	b.initSummarizer()
	b.initCommands()

	return b
}

// -- init blocks ----------------------------------------------------------

func (b *Bot) initConfig() {
	b.cfg, _ = config.Load()
	b.promptBuilder = prompt.NewBuilder(b.cwd)
}

func (b *Bot) initCtxMgr() {
	systemPrompt := b.promptBuilder.Build()
	memFile, _ := memory.Load(memory.DefaultPath())
	b.ctxMgr = ctxmgr.New(ctxmgr.Config{SystemPrompt: systemPrompt, Memory: memFile})

	if b.cwd == "" {
		b.ctxMgr.SetContextWindow(b.cfg.ContextWindow)
		return
	}

	b.projCtx = cindex.LoadProjectContext(b.cwd)
	if b.projCtx != "" {
		b.ctxMgr.Add("system", b.projCtx)
	}

	mgr, err := cindex.NewManager(b.cwd)
	if err != nil {
		b.ctxMgr.SetContextWindow(b.cfg.ContextWindow)
		return
	}
	if err := mgr.Init(); err != nil {
		b.ctxMgr.SetContextWindow(b.cfg.ContextWindow)
		return
	}

	if g := mgr.Graph(); g != nil {
		if skeleton := g.FormatSkeleton(b.cwd); skeleton != "" {
			b.ctxMgr.Add("system", skeleton)
		}
	}

	b.cindexMgr = mgr
	b.ctxMgr.SetContextWindow(b.cfg.ContextWindow)
}

func (b *Bot) initSummarizer() {
	b.ctxMgr.CM.Summarizer = ctxmgr.MakeSummarizer(b.ctxMgr.CM.CancelCtx, b.ctxMgr.MergeClient)
}

func (b *Bot) initToolRegistry() {
	b.toolRegistry = tools.NewRegistry()
	builtin.RegisterAll(b.toolRegistry, b.cfg.ImageGenModels)

	if b.cindexMgr != nil {
		b.toolRegistry.Register(cindex.NewProjectInfoTool(b.cindexMgr))
	}

	tools.SetGlobalFileCache(tools.NewFileStateCache())
	tools.SetGlobalSnapshotStore(hashline.NewSnapshotStore())
	builtin.InitBlockResolver()
}

func (b *Bot) initSkills() {
	b.reloadSkills(nil)
}

// reloadSkills reinitializes the skill registry from bundled skills and plugin dirs.
// If loaded is non-nil, it's used for the loaded-set; otherwise nil is passed.
func (b *Bot) reloadSkills(loaded map[string]bool) {
	b.skillReg = skill.NewRegistry()
	b.skillReg.RegisterBundled(bundled.All())
	dirs := skill.DefaultDirs()
	for _, d := range b.pluginReg.SkillDirs() {
		dirs = append(dirs, d)
	}
	if err := b.skillReg.Load(dirs); err != nil {
		fmt.Fprintf(os.Stderr, "skill: load error: %v\n", err)
	}
	b.ctxMgr.SetSkillList(skill.BuildSkillListText(b.skillReg.List(), loaded, b.cfg.ContextWindow))
	b.toolRegistry.Unregister("skill")
	b.toolRegistry.Register(skill.NewSkillTool(b.skillReg))
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
		cfg.Env = expandMCPEnv(cfg.Env, p.Dir)
		client := mcp.NewClient(name, cfg)
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
	b.hookReg.UnregisterWhere(func(h hooks.Hook) bool {
		return strings.HasPrefix(h.Name, "plugin:") && strings.Contains(h.Name, p.Dir)
	})
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

func (b *Bot) initSession() {
	b.cmdParser = command.NewParser()
	b.skillState = &command.SkillState{MsgStart: -1}

	sess, err := session.New(b.cwd)
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
		// Only inject when the count crosses the threshold AND has grown
		// significantly since the last injection. This prevents the guardrail
		// from nagging the model on every single turn once 40 is reached.
		if toolResults > 40 && toolResults-b.lastGuardrailWarned >= 10 {
			b.lastGuardrailWarned = toolResults
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
		t.(*builtin.TaskTool).Wire(func(ctx context.Context, prompt, agentType, thoroughness string) (*subagent.Result, error) {
			at, ok := subagent.Get(agentType)
			if !ok {
				return nil, fmt.Errorf("unknown sub-agent type: %s", agentType)
			}
			cfg := subagent.RunConfig{
				Prompt:          prompt,
				AgentType:       at,
				Cwd:             b.cwd,
				ProjectContext:  b.projCtx,
				Thoroughness:    thoroughness,
				ContextWindow:   b.cfg.ContextWindow,
				DisableThinking: true,
				ConfirmFn:       b.ag.ConfirmFn(),
			}
			// Wire sub-callback from context (injected by run_exec.go for TUI forwarding).
			if subCB, ok := subagent.SubCallbackFromCtx(ctx); ok {
				cfg.OnToolCall = func(ev subagent.ToolCallEvent) {
					subCB("sub_"+ev.Action, ev.ToolName, ev.ToolArgs, ev.Output)
				}
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
			return command.ForceFreshStart(b.ctxMgr, b.skillReg, b.hookReg, b.cfg.ContextWindow)
		},
		SwitchModel: b.SwitchModel,
	}, b.skillState)

	b.cmdParser.Register("sessions", func(cmd *command.Command) (string, bool) {
		if len(cmd.Args) > 0 {
			id := cmd.Args[0]
			if err := b.ResumeSession(id); err != nil {
				return fmt.Sprintf("Failed to resume session %s: %v", id, err), true
			}
			b.sessionResumed = true
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
		path := "/tmp/nekocode/nekocode-context.json"
		if err := common.WriteFileWithDir(path, data, 0o644); err != nil {
			return fmt.Sprintf("Failed to write file: %v", err), true
		}
		return fmt.Sprintf("Context exported to %s (%d messages)", path, len(msgs)), true
	})

	b.registerPluginCommands()
}

// -- session persistence --------------------------------------------------

func (b *Bot) saveSession() {
	if b.sess == nil {
		return
	}
	snap := b.ctxMgr.Snapshot()
	b.sess.SystemPrompt = snap.SystemPrompt
	b.sess.Skills = snap.Skills
	b.sess.Memory = snap.Memory
	b.sess.Archive = snap.Archive
	b.sess.Messages = snap.Messages
	b.sess.CompactBoundary = snap.CompactBoundary
	b.sess.ContextWindow = snap.Budget
	b.mu.Lock()
	b.sess.PromptTokens, b.sess.CompletionTokens = b.ag.TokenUsage()
	b.mu.Unlock()
	b.sess.LoadedSkills = b.sess.LoadedSkills[:0]
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
	b.ctxMgr.Restore(ctxmgr.ManagerSnapshot{
		SystemPrompt:    sess.SystemPrompt,
		Skills:          sess.Skills,
		Archive:         sess.Archive,
		Memory:          sess.Memory,
		CompactBoundary: sess.CompactBoundary,
		Messages:        sess.Messages,
		Budget:          sess.ContextWindow,
	})
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
