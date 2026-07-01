package runtime

import (
	"context"

	"nekocode/bot/agent/runtime/toolrun"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/hooks"
	"nekocode/bot/llm/types"
	aggov "nekocode/bot/policy"
	"nekocode/bot/tools"
)

type runnerHost struct {
	agent *Agent
}

func (h runnerHost) Context() context.Context {
	return h.agent.getCtx()
}

func (h runnerHost) ContextManager() *ctxmgr.Manager {
	return h.agent.deps.ctxMgr
}

func (h runnerHost) Executor() *tools.Executor {
	return h.agent.deps.executor
}

func (h runnerHost) Governance() *aggov.Manager {
	return h.agent.deps.gov
}

func (h runnerHost) SubSlots() *toolrun.SlotManager {
	return h.agent.deps.subSlotMgr
}

func (h runnerHost) InjectHint(hint *hooks.Hint) {
	h.agent.injectHint(hint)
}

func (h runnerHost) IncStep() {
	h.agent.run.step++
}

func (h runnerHost) StopPostTool(reason hooks.StopReason) {
	h.agent.run.stopReason = reason
	h.agent.run.lastText = ""
}

func (h runnerHost) LLM() types.LLM {
	return h.agent.deps.llmClient
}

func (h runnerHost) ToolRegistry() *tools.Registry {
	return h.agent.deps.toolRegistry
}

func (h runnerHost) IsFinished() bool {
	return h.agent.life.finished.Load()
}

func (h runnerHost) LastReason() string {
	return h.agent.stream.lastReason
}

func (h runnerHost) SetLastReason(reason string) {
	h.agent.stream.lastReason = reason
}

func (h runnerHost) Phase(phase string) {
	h.agent.stream.emitPhase(phase)
}

func (h runnerHost) StreamText(delta string) {
	h.agent.stream.emitText(delta)
}

func (h runnerHost) StreamReasoning(delta string) {
	h.agent.stream.emitReasoning(delta)
}

func (h runnerHost) AddTokens(prompt, completion int) {
	h.agent.AddTokens(prompt, completion)
}
