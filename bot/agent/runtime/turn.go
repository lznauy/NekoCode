package runtime

import (
	"nekocode/bot/agent/runtime/messages"
	"nekocode/bot/agent/runtime/toolrun"
	"nekocode/bot/hooks"
	aggov "nekocode/bot/policy"
	"nekocode/bot/policy/budget"
	"nekocode/bot/tools"
)

type turnRunner struct {
	agent *Agent
}

func newTurnRunner(agent *Agent) *turnRunner {
	return &turnRunner{agent: agent}
}

func (r *turnRunner) prepareTurn(input string) budget.ToolQuota {
	a := r.agent
	a.deps.ctxMgr.AutoCompactIfNeeded()
	quota := budget.ComputeQuota(a.deps.ctxMgr.TokenUsage())
	r.applyPreTurnHooks(input, quota)
	return quota
}

func (r *turnRunner) applyPreTurnHooks(input string, quota budget.ToolQuota) {
	a := r.agent
	if a.deps.gov == nil || a.deps.gov.HookReg == nil {
		a.applyTurnHints(nil)
		return
	}
	a.deps.gov.ResetTurnBetween(input, aggov.QuotaData{
		MaxSlots: quota.MaxSlots,
		Used:     quota.Used,
	})
	a.deps.gov.HookReg.Set(hooks.StoreTasksAllDone, boolStore(a.deps.ctxMgr.AllTasksDone()))
	a.deps.gov.HookReg.Set(hooks.StoreHasTasks, boolStore(a.deps.ctxMgr.HasTasks()))

	var hints []hooks.Hint
	for _, r := range a.deps.gov.HookReg.Evaluate(hooks.PreTurn, "", false) {
		if r.Hint != nil {
			hints = append(hints, *r.Hint)
		}
	}
	a.applyTurnHints(hints)
}

func boolStore(ok bool) int64 {
	if ok {
		return 1
	}
	return 0
}

func (r *turnRunner) interruptedBeforeReasoning(callback RunCallback) bool {
	a := r.agent
	a.drainSteering()
	if a.getCtx().Err() == nil {
		return false
	}
	a.run.stopReason = hooks.StopInterrupted
	a.run.lastText = messages.MsgInterrupted
	if callback != nil {
		callback("chat", "", "", messages.MsgInterrupted)
	}
	return true
}

func (r *turnRunner) retryAfterInterruptedReasoning(reasoning *ReasoningResult, msgCountBefore int) bool {
	a := r.agent
	if !reasoning.Interrupted {
		return false
	}
	if a.life.finished.Load() {
		a.run.stopReason = hooks.StopInterrupted
		return false
	}
	// Count interrupted responses toward the step limit to prevent
	// unbounded loops when the LLM repeatedly produces interrupted output.
	a.run.step++
	a.deps.ctxMgr.TruncateTo(msgCountBefore)
	a.drainSteering()
	return true
}

func (r *turnRunner) handleToolCalls(calls []tools.ToolCallItem, reasoning *ReasoningResult, quota *budget.ToolQuota, callback RunCallback) bool {
	a := r.agent
	a.run.consecutiveHints = 0
	a.run.consecutiveFailures = 0
	a.run.gate.Reset()
	return a.toolRunner.ExecuteAndFeedback(calls, reasoning.TextContent, quota, toolrun.Callback(callback))
}
