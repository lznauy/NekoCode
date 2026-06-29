package app

import (
	"os"
	"sync"

	"nekocode/bot/agent/runtime"
	"nekocode/bot/command"
	"nekocode/bot/config"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/mcp"
	"nekocode/bot/hooks"
	"nekocode/bot/index/service"
	"nekocode/bot/plugin"
	"nekocode/bot/prompt"
	"nekocode/bot/session"
	"nekocode/bot/skill"
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
	ag           *runtime.Agent
	toolRegistry *tools.Registry
	hookReg      *hooks.Registry
}

type extensionRuntime struct {
	skills     *skill.Manager
	plugins    *plugin.Manager
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
	sessions       *session.Manager
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
