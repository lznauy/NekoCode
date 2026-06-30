package app

import (
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/mcp"
	"nekocode/bot/extension/plugin"
	"nekocode/bot/extension/skill"
	"nekocode/bot/hooks"
	"nekocode/bot/tools"
	"nekocode/common"
)

type extensionFacade struct {
	skills     *skill.Manager
	plugins    *plugin.Manager
	mcpClients map[string]*mcp.Client
	mcpHealth  map[string]mcpHealth
	configMCP  []common.MCPServerView

	ctxMgr        *ctxmgr.Manager
	toolRegistry  *tools.Registry
	hookReg       *hooks.Registry
	contextWindow int
	cb            *callbackBus
}

type mcpHealth struct {
	Status    string
	Error     string
	ToolCount int
}

type extensionDeps struct {
	CtxMgr        *ctxmgr.Manager
	ToolRegistry  *tools.Registry
	HookReg       *hooks.Registry
	ContextWindow int
	Callbacks     *callbackBus
}

func (e *extensionFacade) Init(d extensionDeps) {
	e.ctxMgr = d.CtxMgr
	e.toolRegistry = d.ToolRegistry
	e.hookReg = d.HookReg
	e.contextWindow = d.ContextWindow
	e.cb = d.Callbacks
	e.mcpClients = make(map[string]*mcp.Client)
	e.mcpHealth = make(map[string]mcpHealth)
}
