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
	cfg                 *config.Config
	ctxMgr              *ctxmgr.Manager
	cmdParser           *command.Parser
	skillState          *command.SkillState
	ag                  *agent.Agent
	sess                *session.Snapshot
	skillReg            *skill.Registry
	pluginReg           *plugin.Registry
	mcpClients          map[string]*mcp.Client
	confirmFn           common.ConfirmFunc
	phaseFn             common.PhaseFunc
	todoFn              common.TodoFunc
	notifyFn            func(string)
	confirmCh           chan common.ConfirmRequest
	confirmMu           sync.Mutex
	pendingConfirm      bool
	promptBuilder       *prompt.Builder
	toolRegistry        *tools.Registry
	hookReg             *hooks.Registry
	projCtx             string
	indexMgr            *service.Manager
	cwd                 string
	lastGuardrailWarned int
	sessionResumed      bool
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
