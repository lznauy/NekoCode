package app

import (
	"sync"

	"nekocode/bot/agent/runtime"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/plugin"
	"nekocode/bot/tools"
	"nekocode/common"
)

type callbackBus struct {
	confirmFn common.ConfirmFunc
	phaseFn   common.PhaseFunc
	todoFn    common.TodoFunc
	notifyFn  func(string)
	confirmCh chan common.ConfirmRequest

	confirmMu      sync.Mutex
	pendingConfirm bool

	toolRegistry *tools.Registry
	ctxMgr       *ctxmgr.Manager
	getAgent     func() *runtime.Agent
}

type callbackDeps struct {
	ToolRegistry *tools.Registry
	CtxMgr       *ctxmgr.Manager
	GetAgent     func() *runtime.Agent
}

func (c *callbackBus) Init(d callbackDeps) {
	c.toolRegistry = d.ToolRegistry
	c.ctxMgr = d.CtxMgr
	c.getAgent = d.GetAgent
}

func (c *callbackBus) Configure(confirmFn common.ConfirmFunc, phaseFn common.PhaseFunc, todoFn common.TodoFunc, notifyFn func(string), confirmCh chan common.ConfirmRequest, questionFn common.QuestionFunc) {
	c.configureCallbacks(confirmFn, phaseFn, todoFn, notifyFn, confirmCh)
	c.setQuestionFunc(questionFn)
}

func (c *callbackBus) configureCallbacks(confirmFn common.ConfirmFunc, phaseFn common.PhaseFunc, todoFn common.TodoFunc, notifyFn func(string), confirmCh chan common.ConfirmRequest) {
	c.confirmFn = confirmFn
	c.phaseFn = phaseFn
	c.todoFn = todoFn
	c.notifyFn = notifyFn
	c.confirmCh = confirmCh
	c.applyAgentCallbacks()
}

func (c *callbackBus) setQuestionFunc(fn common.QuestionFunc) {
	if c.toolRegistry == nil {
		return
	}
	t, err := c.toolRegistry.Get("question")
	if err != nil {
		return
	}
	if qt, ok := t.(interface{ SetQuestionFunc(common.QuestionFunc) }); ok {
		qt.SetQuestionFunc(fn)
	}
}

func (c *callbackBus) SetCallbacks(textFn, reasonFn func(string)) {
	if c.getAgent == nil {
		return
	}
	ag := c.getAgent()
	if ag == nil {
		return
	}
	if textFn != nil {
		ag.SetStreamFn(func(delta string, _ bool) { textFn(delta) })
	}
	if reasonFn != nil {
		ag.SetReasoningStreamFn(reasonFn)
	}
}

func (c *callbackBus) applyAgentCallbacks() {
	if c.getAgent == nil {
		return
	}
	ag := c.getAgent()
	c.applyAgentCallbacksTo(ag)
}

func (c *callbackBus) applyAgentCallbacksTo(ag *runtime.Agent) {
	if ag == nil {
		return
	}
	ag.SetConfirmFn(c.confirmFn)
	ag.SetPhaseFn(c.phaseFn)
	ag.WireTodoWrite(func(items []common.TodoItem) {
		if c.ctxMgr != nil {
			c.ctxMgr.SetTodos(items)
		}
		if c.todoFn != nil {
			c.todoFn(items)
		}
	})
}

func (c *callbackBus) pendingConfirmation() bool {
	c.confirmMu.Lock()
	defer c.confirmMu.Unlock()
	return c.pendingConfirm
}

func (c *callbackBus) setPendingConfirmation(pending bool) {
	c.confirmMu.Lock()
	c.pendingConfirm = pending
	c.confirmMu.Unlock()
}

func (c *callbackBus) UnblockConfirm() {
	c.setPendingConfirmation(false)
	if c.confirmCh != nil {
		select {
		case c.confirmCh <- common.ConfirmRequest{Response: nil}:
		default:
		}
	}
}

func (c *callbackBus) ConfirmInstall(source string, p *plugin.Plugin, isRemote bool) bool {
	summary := plugin.ConfirmSummary(p, isRemote)
	if c.confirmFn == nil {
		c.UnblockConfirm()
		return false
	}
	result := c.confirmFn(common.NewConfirmRequest("/plugin install", map[string]any{"source": source, "summary": summary}, common.LevelWrite))
	c.setPendingConfirmation(false)
	if !result && c.notifyFn != nil {
		c.notifyFn("Install cancelled: " + source)
	}
	return result
}

func (c *callbackBus) InstallCallbacks() plugin.InstallCallbacks {
	return plugin.InstallCallbacks{
		Confirm:    c.ConfirmInstall,
		Notify:     c.notifyFn,
		SetPending: c.setPendingConfirmation,
		Unblock:    c.UnblockConfirm,
	}
}
