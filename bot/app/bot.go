package app

import (
	"os"
	"sync"

	"nekocode/bot/agent"
	"nekocode/bot/command"
	"nekocode/bot/config"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/mcp"
	"nekocode/bot/extension/plugin"
	"nekocode/bot/extension/skill"
	"nekocode/bot/hooks"
	"nekocode/bot/index/service"
	"nekocode/bot/prompt"
	"nekocode/bot/session"
	"nekocode/bot/tools"
	"nekocode/common"
)

type Bot struct {
	botCore
	botRuntime
	extensionRuntime
	callbackRuntime
	confirmRuntime
	sessionRuntime
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
	ag           *agent.Agent
	toolRegistry *tools.Registry
	hookReg      *hooks.Registry
}

type extensionRuntime struct {
	skillReg   *skill.Registry
	pluginReg  *plugin.Registry
	mcpClients map[string]*mcp.Client
}

type callbackRuntime struct {
	confirmFn common.ConfirmFunc
	phaseFn   common.PhaseFunc
	todoFn    common.TodoFunc
	notifyFn  func(string)
	confirmCh chan common.ConfirmRequest
}

type confirmRuntime struct {
	confirmMu      sync.Mutex
	pendingConfirm bool
}

type sessionRuntime struct {
	sess           *session.Snapshot
	sessionResumed bool
}

type appLock struct {
	mu sync.Mutex
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
