package app

import (
	"context"
	"os"
	"sync"

	"nekocode/bot/agent/runtime"
	"nekocode/bot/command"
	"nekocode/bot/config"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/contextmgr/memory"
	"nekocode/bot/hooks"
	"nekocode/bot/index/projectctx"
	"nekocode/bot/index/projecttool"
	"nekocode/bot/index/service"
	"nekocode/bot/llm"
	systemprompt "nekocode/bot/prompt/system"
	"nekocode/bot/tools"
	"nekocode/bot/tools/catalog"
	"nekocode/common"
)

type Bot struct {
	botCore
	botRuntime
	ext       *extensionFacade
	cb        *callbackBus
	subWiring *subagentWiring
	sess      *sessionFacade
	mu        sync.Mutex
}

type botCore struct {
	cfg           *config.Config
	ctxMgr        *ctxmgr.Manager
	cmdParser     *command.Parser
	skillState    *command.SkillState
	promptBuilder *systemprompt.Builder
	projCtx       string
	indexMgr      *service.Manager
	cwd           string
}

type botRuntime struct {
	ag           *runtime.Agent
	toolRegistry *tools.Registry
	hookReg      *hooks.Registry
}

func New() *Bot {
	b := &Bot{}
	b.cwd, _ = os.Getwd()

	b.initConfig()
	b.initCtxMgr()
	b.cmdParser = command.NewParser()
	b.skillState = &command.SkillState{MsgStart: -1}
	b.initSession()
	b.reinit()

	return b
}

func (b *Bot) initConfig() {
	b.cfg, _ = config.Load()
	b.promptBuilder = systemprompt.NewBuilder(b.cwd)
}

func (b *Bot) initCtxMgr() {
	systemPrompt := b.promptBuilder.Build()
	memFile, _ := memory.Load(memory.DefaultPath())
	b.ctxMgr = ctxmgr.New(ctxmgr.Config{SystemPrompt: systemPrompt, Memory: memFile})

	result := projectctx.Apply(b.ctxMgr, projectctx.ApplyOptions{
		CWD:           b.cwd,
		ContextWindow: b.cfg.ContextWindow,
	})
	b.projCtx = result.ProjectContext
	b.indexMgr = result.IndexManager
}

// reinit rebuilds the runtime facades, agent, summarizer, and commands.
// Called from New() for initial setup and from ApplyConfig() for hot reload.
func (b *Bot) reinit() {
	b.initToolRegistry()
	b.initHooks()
	if b.cb == nil {
		b.cb = &callbackBus{}
	}
	b.ext = newExtensionFacade(b.ctxMgr, b.toolRegistry, b.hookReg, b.cfg.ContextWindow)
	b.subWiring = newSubagentWiring(b.toolRegistry, b.ctxMgr, b.cwd, b.projCtx, b.cfg.ContextWindow)
	b.ext.InitPlugins()
	b.ext.InitConfigMCPServers(b.cfg.MCPServers)
	b.ext.InitSkills()
	b.initAgent()
	b.initSummarizer()
	b.initCommands()
}

func (b *Bot) initSummarizer() {
	b.ctxMgr.CM.Summarizer = ctxmgr.MakeSummarizer(b.ctxMgr.CM.CancelCtx, b.ctxMgr.MergeClient)
}

func (b *Bot) initToolRegistry() {
	b.toolRegistry = tools.NewRegistry()
	catalog.RegisterAll(b.toolRegistry, b.cfg.ImageGenModels)

	if b.indexMgr != nil {
		b.toolRegistry.Register(projecttool.NewProjectInfoTool(b.indexMgr))
	}
}

func (b *Bot) initHooks() {
	b.hookReg = hooks.NewRegistry()
	hooks.RegisterBuiltin(b.hookReg)
}

func (b *Bot) initAgent() {
	am := b.cfg.ActiveModelConfig()
	llmClient := llm.NewClientWithProtocol(am.Provider, am.APIKey, am.BaseURL, am.Model, am.Protocol)

	fm := b.cfg.ResolveModel(b.cfg.FlashModel)
	mergeClient := llm.NewClientWithProtocol(fm.Provider, fm.APIKey, fm.BaseURL, fm.Model, fm.Protocol)
	mergeClient.SetDisableThinking(true)
	mergeClient.SetMaxTokens(2000)
	b.ctxMgr.MergeClient = mergeClient

	b.ag = runtime.New(context.Background(), b.ctxMgr, llmClient, b.toolRegistry)
	b.ag.SetHookRegistry(b.hookReg)
	b.applyCallbacks()

	b.subWiring.WireTaskTool(fm, b.ag)
}

func (b *Bot) applyCallbacks() {
	b.cb.applyAgentControlCallbacksTo(b.ag)
	b.ag.WireTodoWrite(func(items []common.TodoItem) {
		b.ctxMgr.SetTodos(items)
		b.cb.todoWriter()(items)
	})
	b.setQuestionFunc(b.cb.questionFn)
}

func (b *Bot) setQuestionFunc(fn common.QuestionFunc) {
	if fn == nil || b.toolRegistry == nil {
		return
	}
	t, err := b.toolRegistry.Get("question")
	if err != nil {
		return
	}
	if qt, ok := t.(interface{ SetQuestionFunc(common.QuestionFunc) }); ok {
		qt.SetQuestionFunc(fn)
	}
}

func (b *Bot) initCommands() {
	skills := skillCommandProvider{manager: b.ext.skills}
	command.RegisterAll(b.cmdParser, command.Deps{
		CtxMgr:        b.ctxMgr,
		Ag:            func() command.PlanModeController { return b.getAgent() },
		Skills:        skills,
		ToolRegistry:  b.toolRegistry,
		ContextWindow: b.cfg.ContextWindow,
		GetConfigFn:   b.ProviderModel,
		ListModelsFn:  b.cfg.AllModelNames,
		FreshStart: func() (string, error) {
			return command.ForceFreshStart(b.ctxMgr, skills, b.hookReg)
		},
		SwitchModel: b.SwitchModel,
	}, b.skillState)

	b.ext.RegisterPluginCommands(b.cmdParser, b.cb.InstallCallbacks())
	b.sess.RegisterCommands(b.cmdParser)
}
