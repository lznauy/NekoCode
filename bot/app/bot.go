package app

import (
	"os"
	"sync"

	"nekocode/bot/agent/runtime"
	"nekocode/bot/command"
	"nekocode/bot/config"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/hooks"
	"nekocode/bot/index/service"
	"nekocode/bot/prompt"
	"nekocode/bot/tools"
)

type Bot struct {
	botCore
	botRuntime
	sessionRuntime
	ext       *extensionFacade
	cb        *callbackBus
	subWiring *subagentWiring
	appLock
}

type botCore struct {
	cfg                 *config.Config
	ctxMgr              *ctxmgr.Manager
	cmdParser           *command.Parser
	skillState          *command.SkillState
	promptBuilder       *prompt.Builder
	projCtx             string
	indexMgr            *service.Manager
	cwd                 string
	lastGuardrailWarned int
}

type botRuntime struct {
	ag           *runtime.Agent
	toolRegistry *tools.Registry
	hookReg      *hooks.Registry
}

type sessionRuntime struct {
	sess *sessionFacade
}

type appLock struct {
	mu sync.Mutex
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

// reinit rebuilds the runtime facades, agent, summarizer, and commands.
// Called from New() for initial setup and from ApplyConfig() for hot reload.
func (b *Bot) reinit() {
	b.initToolRegistry()
	b.initHooks()
	b.cb = &callbackBus{}
	b.cb.Init(callbackDeps{
		ToolRegistry: b.toolRegistry,
		CtxMgr:       b.ctxMgr,
		GetAgent:     b.getAgent,
	})
	b.ext = &extensionFacade{}
	b.ext.Init(extensionDeps{
		CtxMgr:        b.ctxMgr,
		ToolRegistry:  b.toolRegistry,
		HookReg:       b.hookReg,
		ContextWindow: b.cfg.ContextWindow,
		CmdParser:     b.cmdParser,
		Callbacks:     b.cb,
	})
	b.subWiring = &subagentWiring{}
	b.subWiring.Init(subagentWiringDeps{
		ToolRegistry:  b.toolRegistry,
		CtxMgr:        b.ctxMgr,
		CWD:           b.cwd,
		ProjectCtx:    b.projCtx,
		ContextWindow: b.cfg.ContextWindow,
		GetAgent:      b.getAgent,
	})
	b.ext.InitPlugins()
	b.ext.InitConfigMCPServers(b.cfg.MCPServers)
	b.ext.InitSkills()
	b.initAgent()
	b.initSummarizer()
	b.initCommands()
}
