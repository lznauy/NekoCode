// Package core assembles NekoCode's transport-neutral agent foundation.
package core

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"nekocode/bot/agent"
	"nekocode/bot/checkpoint"
	"nekocode/bot/command"
	"nekocode/bot/config"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/contextmgr/memory"
	"nekocode/bot/decision"
	"nekocode/bot/extension"
	"nekocode/bot/extension/tool/builtin/catalog"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/bot/policy"
	"nekocode/bot/policy/builtin"
	"nekocode/bot/project"
	"nekocode/bot/prompt"
	"nekocode/bot/provider"
	"nekocode/bot/session"
	"nekocode/logger"
	"nekocode/protocol"
)

// RunHost is the synchronous boundary used by one bot run. Runtime and other
// application layers adapt this port without leaking their view models into
// the bot.
type RunHost interface {
	Text(string)
	Reason(string)
	Step(protocol.StepEvent)
	Phase(string)
	Todos([]protocol.TodoItem)
	Confirm(protocol.ConfirmRequest) protocol.ConfirmReply
	Ask(protocol.QuestionRequest) protocol.QuestionReply
}

type Bot struct {
	cwd             string
	home            string
	cfg             *config.Config
	promptBuilder   *prompt.Builder
	project         *project.Project
	ctxMgr          *ctxmgr.Manager
	policy          *policy.Policy
	ag              *agent.Agent
	toolbox         *catalog.Toolbox
	cmd             *command.Handler
	ext             *extension.Manager
	sess            *session.Manager
	checkpoints     *checkpoint.Manager
	mcpAuthNotifier func(message string)
	mu              sync.Mutex
	projectReloadMu sync.Mutex
	reloadView      *extension.Snapshot // Previous complete view while extensions reload.
	hostMu          sync.RWMutex
	runHost         RunHost
	// Lock-free snapshot for menu/status reads that already hold b.mu.
	permissionMode atomic.Int32
	jevAvailable   atomic.Bool
	riskJudge      decision.RiskJudge
}

// New assembles the standard bot and loads its persisted configuration,
// memory, and current session.
func New() (*Bot, error) {
	b := &Bot{}
	var err error
	if b.cwd, err = os.Getwd(); err != nil {
		return nil, fmt.Errorf("bot: resolve working directory: %w", err)
	}
	if b.home, err = os.UserHomeDir(); err != nil {
		return nil, fmt.Errorf("bot: resolve home directory: %w", err)
	}
	if err := b.initConfig(); err != nil {
		return nil, err
	}
	if err := b.initCtxMgr(); err != nil {
		return nil, err
	}
	b.initSession()
	if err := b.rebuildRuntime(); err != nil {
		return nil, err
	}
	return b, nil
}

// Close releases lifecycle resources held by the bot (currently the
// stateful shell tool inside the toolbox).
func (b *Bot) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ext != nil {
		b.ext.Close()
	}
	if b.toolbox == nil {
		return nil
	}
	return b.toolbox.Close()
}

func (b *Bot) initConfig() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("bot: load config: %w", err)
	}
	b.cfg = cfg
	b.project, err = project.New(b.cwd)
	if err != nil {
		return fmt.Errorf("bot: resolve project: %w", err)
	}
	b.promptBuilder = prompt.New(b.cwd)
	b.promptBuilder.SetEnvironmentProvider(b.environment)
	b.reloadProject()
	return nil
}

// jevSettings maps the user-facing config onto the decision engine settings.
// An absent section uses the environment key when present; without either
// key, the optional decision engines remain disabled.
func jevSettings(cfg *config.JevConfig) decision.JevSettings {
	if cfg == nil {
		return decision.JevSettings{}
	}
	return decision.JevSettings{
		APIKey:        cfg.APIKey,
		Model:         cfg.Model,
		BaseURL:       cfg.BaseURL,
		KeepThreshold: cfg.KeepThreshold,
		Enabled:       cfg.Enabled,
	}
}

func (b *Bot) initCtxMgr() error {
	systemPrompt := b.promptBuilder.BuildStatic()
	memFile, err := memory.Load(memory.DefaultPath())
	if err != nil {
		return fmt.Errorf("bot: load memory: %w", err)
	}
	b.ctxMgr = ctxmgr.New(ctxmgr.Config{
		SystemPrompt:       systemPrompt,
		ContextWindow:      b.cfg.EffectiveContextWindow(),
		AutoCompactPercent: b.cfg.EffectiveAutoCompactPercent(),
		Memory:             memFile,
		RuntimePrompt:      b.promptBuilder.BuildEnvironment,
	})
	b.ctxMgr.SetCompactionObserver(func(event protocol.CompactionEvent) {
		if host := b.currentHost(); host != nil {
			host.Step(protocol.StepEvent{Action: protocol.StepActionCompaction, Compaction: &event})
		}
	})
	return nil
}

// rebuildRuntime recreates components affected by configuration changes.
// Keep this order: tools and policy are dependencies of extensions and agent;
// commands consume all of them.
func (b *Bot) rebuildRuntime() error {
	if b.ext != nil {
		b.ext.Close()
	}
	settings := jevSettings(b.cfg.Jev)
	b.ctxMgr.SetToolPruner(decision.NewToolPruner(settings))
	b.riskJudge = decision.NewRiskJudge(settings)
	b.jevAvailable.Store(b.riskJudge != nil)
	if !b.jevAvailable.Load() && b.BashAuto() {
		b.permissionMode.Store(int32(permission.ModeManual))
		logger.Log("permission mode changed to manual: Jev judge unavailable")
	}
	b.initToolRegistry()
	b.initPolicy()

	b.toolbox.Workspace().Configure(b.cwd, b.configuredWorkspaceRoots())
	b.initExtensions()
	b.initAgent()
	// A fresh agent means a fresh executor: the full-takeover mode does not
	// carry over (e.g. after a model switch), so reset the lock-free mirror.
	if b.FullAccess() {
		b.setPermissionModeLocked(permission.ModeManual)
	}
	b.initCommands()
	return nil
}

func (b *Bot) configuredWorkspaceRoots() []workspace.Root {
	var roots []workspace.Root
	if b.cfg != nil {
		for _, r := range b.cfg.Workspaces {
			access := workspace.Access(r.Access)
			if access == "" {
				access = workspace.AccessReadOnly
			}
			roots = append(roots, workspace.Root{Path: r.Path, Access: access})
		}
	}
	for _, r := range permission.NewStore(b.cwd).WorkspaceRoots() {
		roots = append(roots, workspace.Root{Path: r.Path, Access: workspace.Access(r.Access)})
	}
	return roots
}

func (b *Bot) initToolRegistry() {
	if b.toolbox == nil {
		b.toolbox = catalog.NewToolbox(b.cfg.ImageGenModels)
		return
	}
	b.toolbox.RebuildRegistry(b.cfg.ImageGenModels)
}

func (b *Bot) initPolicy() {
	b.policy = policy.New()
	b.syncPolicySessionID()
	builtin.Register(b.policy)
}

func (b *Bot) initAgent() {
	am := b.cfg.ActiveModelConfig()
	llmClient := provider.New(provider.Config{
		APIKey: am.APIKey, BaseURL: am.BaseURL, Model: am.Model, Protocol: am.Protocol,
		Reasoning: resolvedReasoning(am),
	})

	fm := b.cfg.ResolveModel(b.cfg.FlashModel)
	compactionModel := provider.New(provider.Config{
		APIKey: fm.APIKey, BaseURL: fm.BaseURL, Model: fm.Model, Protocol: fm.Protocol,
		Reasoning: resolvedReasoning(fm),
	})
	compactionModel.SetDisableThinking(true)
	compactionModel.SetMaxTokens(2000)
	b.ctxMgr.ConfigureModel(ctxmgr.ModelContext{
		Window: b.cfg.EffectiveContextWindow(), AutoCompactPercent: b.cfg.EffectiveAutoCompactPercent(),
		CompactionModel: compactionModel, Reasoning: resolvedReasoning(am),
	})

	b.ag = agent.New(context.Background(), agent.Config{
		Context:     b.ctxMgr,
		Model:       llmClient,
		Tools:       b.toolbox.Registry,
		Policy:      b.policy,
		Checkpoints: b.checkpoints,
		SessionID:   b.sess.CurrentID,
		Output: agent.Output{
			Text:   b.stream,
			Reason: b.reasoning,
			Phase:  b.phase,
		},
		Interaction: agent.Interaction{
			Confirm: b.confirm,
			Ask:     b.ask,
			Todos: func(items []protocol.TodoItem) {
				b.ctxMgr.SetTodos(items)
				b.todos(items)
			},
		},
	})
	ask, todos := b.ag.ToolInteraction()
	b.toolbox.WireInteraction(ask, todos)
	b.configureAgent(b.ag)

	// Auto permission mode: re-apply the risk judge and mode flag to the
	// fresh executor (agents are rebuilt on config changes).
	b.ag.Executor().SetBashAutoJudge(bashRiskJudgeFn(b.riskJudge))
	if web, ok := b.riskJudge.(decision.URLRiskJudge); ok {
		b.ag.Executor().SetWebAutoJudge(webRiskJudgeFn(web))
	}
	if ctxAware, ok := b.riskJudge.(decision.RiskContextAware); ok {
		ctxAware.SetRiskContext(b.cwd)
		b.ag.Executor().SetShellExecutedHook(ctxAware.NoteExecuted)
	}
	// Restore the mode on the fresh executor. Full-takeover is restored on
	// purpose: agent-only rebuilds (model/effort switch) must preserve it —
	// only rebuildRuntime downgrades Full after a full configuration change.
	b.ag.Executor().SetPermissionMode(permission.Mode(b.permissionMode.Load()))

	// Inject the permission engine into the shell tool so builtin sandbox
	// rules (e.g. pnpm dev → network) are applied.
	b.toolbox.SetSandboxEngine(b.ag.SandboxEngine())

	b.wireTaskTool(fm, compactionModel, b.ag)
}

// bashRiskJudgeFn adapts the stored RiskJudge into the executor's judge
// signature; a nil judge stays nil so auto mode fails closed to asking. ctx
// is the calling tool's context, so a slow remote judge honors cancellation.
func bashRiskJudgeFn(judge decision.RiskJudge) func(ctx context.Context, command string) (dangerous bool, ok bool) {
	if judge == nil {
		return nil
	}
	return func(ctx context.Context, command string) (dangerous bool, ok bool) {
		return judge.Dangerous(ctx, command)
	}
}

// webRiskJudgeFn adapts the URL half of the judge for the auto-mode web gate.
func webRiskJudgeFn(judge decision.URLRiskJudge) func(ctx context.Context, url string) (dangerous bool, ok bool) {
	return func(ctx context.Context, url string) (dangerous bool, ok bool) {
		return judge.DangerousURL(ctx, url)
	}
}

// FullAccess reports the current permission mode without taking b.mu, safe
// for status reads from menus and UI refresh paths.
func (b *Bot) FullAccess() bool {
	return permission.Mode(b.permissionMode.Load()) == permission.ModeFull
}

// SetFullAccess preserves auto when full was already off, but leaving full
// always returns to manual. All mode transitions synchronize the executor.
func (b *Bot) SetFullAccess(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if on {
		b.setPermissionModeLocked(permission.ModeFull)
	} else if b.FullAccess() {
		b.setPermissionModeLocked(permission.ModeManual)
	}
}

func (b *Bot) SetBashAuto(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if on {
		if !b.jevAvailable.Load() {
			return
		}
		b.setPermissionModeLocked(permission.ModeAuto)
	} else if b.BashAuto() {
		b.setPermissionModeLocked(permission.ModeManual)
	}
}

func (b *Bot) CanBashAuto() bool { return b.jevAvailable.Load() }

func (b *Bot) setPermissionModeLocked(mode permission.Mode) {
	b.ag.Executor().SetPermissionMode(mode)
	b.permissionMode.Store(int32(mode))
	logger.Log("permission mode changed: %d", mode)
}

func (b *Bot) BashAuto() bool { return permission.Mode(b.permissionMode.Load()) == permission.ModeAuto }

func (b *Bot) initCommands() {
	deps := command.Deps{
		CtxMgr:             b.ctxMgr,
		SetPlanMode:        func(enabled bool) { b.getAgent().Executor().SetPlanMode(enabled) },
		SetFullAccess:      b.SetFullAccess,
		GetFullAccess:      b.FullAccess,
		SetBashAuto:        b.SetBashAuto,
		GetBashAuto:        b.BashAuto,
		CanBashAuto:        b.CanBashAuto,
		ToolRegistry:       b.toolbox.Registry,
		GetConfigFn:        b.model,
		ListModelsFn:       b.cfg.AllModelNames,
		SwitchModel:        b.SwitchModel,
		SetReasoningEffort: b.SetReasoningEffort,
		ResetConversation:  b.resetConversation,
		Rewind:             b.rewindCheckpoint,
	}
	if b.cmd == nil {
		b.cmd = command.New(deps)
	} else {
		b.cmd.RegisterAll(deps)
	}
	b.ext.RegisterCommands(b.cmd, b.confirmInstall)
	b.registerSessionCommands(b.cmd.Parser())
	b.registerWorkspaceCommands(b.cmd.Parser())
	b.registerCommandMenus(b.cmd.Parser())
}
