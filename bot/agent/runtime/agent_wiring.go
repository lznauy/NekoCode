package runtime

import (
	"nekocode/bot/hooks"
	aggov "nekocode/bot/policy"
	"nekocode/bot/tools"

	"nekocode/common"
)

func (a *Agent) SetStreamFn(fn StreamCallback)             { a.textFn = fn }
func (a *Agent) SetReasoningStreamFn(fn ReasoningCallback) { a.reasonFn = fn }

func (a *Agent) SetGovernanceManager(gov *aggov.Manager) { a.gov = gov }
func (a *Agent) GovernanceManager() *aggov.Manager       { return a.gov }

// SetHookRegistry wires the hook registry into the agent's govManager.
// If no manager exists yet, one is created.
func (a *Agent) SetHookRegistry(m *hooks.Registry) {
	if a.gov == nil {
		a.gov = aggov.NewManager(m)
	} else {
		a.gov.HookReg = m
	}
}

func (a *Agent) SetConfirmFn(fn common.ConfirmFunc) { a.executor.SetConfirmFn(fn) }
func (a *Agent) ConfirmFn() common.ConfirmFunc      { return a.executor.ConfirmFn() }
func (a *Agent) SetPhaseFn(fn common.PhaseFunc)     { a.phase = fn; a.executor.SetPhaseFn(fn) }
func (a *Agent) PhaseFn() common.PhaseFunc          { return a.phase }
func (a *Agent) SetPlanMode(on bool)                { a.executor.SetPlanMode(on) }

func (a *Agent) ToolExecutionState() *tools.ExecutionState {
	return a.executor.ExecutionState()
}

func (a *Agent) WireTodoWrite(fn common.TodoFunc) {
	if t, err := a.toolRegistry.Get("todo_write"); err == nil {
		if updater, ok := t.(interface{ SetUpdateFn(common.TodoFunc) }); ok {
			updater.SetUpdateFn(fn)
		}
	}
}
func (a *Agent) AddTokens(prompt, completion int) {
	a.promptTok.Add(int64(prompt))
	a.complTok.Add(int64(completion))
}

func (a *Agent) TokenUsage() (prompt, completion int) {
	return a.ContextTokens(), int(a.complTok.Load())
}

func (a *Agent) TurnTokenUsage() (prompt, completion int) {
	return a.ContextTokens() - int(a.promptSnap), int(a.complTok.Load() - a.complSnap)
}

func (a *Agent) ContextTokens() int {
	_, tokens, _ := a.ctxMgr.Stats()
	return tokens
}
