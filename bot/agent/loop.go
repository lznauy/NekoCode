package agent

import (
	"context"
	"errors"

	"nekocode/bot/agent/internal/kernel"
	"nekocode/bot/contextmgr"
	"nekocode/bot/policy"
	"nekocode/bot/provider/types"
	"nekocode/logger"
	"nekocode/protocol"
)

// maxAgentSteps is the hard ceiling on agent loop iterations. Prevents
// infinite loops when the LLM keeps producing tool calls or Stop hooks
// keep injecting hints without progress.
const maxAgentSteps = 150

// maxConsecutiveHints is the maximum number of consecutive Stop hint
// injections without any intervening tool calls. Beyond this the agent
// is stuck in a text-only loop and should stop.
const maxConsecutiveHints = 3

// maxConsecutiveFailures is the maximum number of consecutive LLM call
// failures (after retries). Beyond this the LLM is likely broken.
const maxConsecutiveFailures = 5

const msgInterrupted = "Interrupted"

type RunResult struct {
	FinalOutput string
	Error       error
	Steps       int
	Interrupted bool
	StopReason  string
}

type RunCallback func(ev protocol.StepEvent)

type loopRunner struct {
	agent        *Agent
	stepLimitHit bool
}

func newLoopRunner(agent *Agent) *loopRunner {
	return &loopRunner{agent: agent}
}

func (r *loopRunner) run(input string, images []types.MessageImage, callback RunCallback) *RunResult {
	return r.runContext(context.Background(), input, images, callback)
}

func (r *loopRunner) runContext(ctx context.Context, input string, images []types.MessageImage, callback RunCallback) *RunResult {
	if err := ctx.Err(); err != nil {
		return &RunResult{Error: err, Interrupted: true}
	}
	a := r.agent
	r.stepLimitHit = false
	beforeRun := a.deps.ctxMgr.Snapshot()
	r.startRun(input, images)
	// Reset in startRun replaces canceled lifecycle contexts. Install the
	// watcher afterwards so startup can never erase this run's cancellation.
	finished, watchDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			a.Abort()
		case <-finished:
		}
	}()
	defer func() { close(finished); <-watchDone }()
	if ctx.Err() != nil {
		a.Abort()
	}
	defer r.logGovernanceSummary()
	if ctx.Err() == nil {
		r.applyUserSubmitHooks()
	}

	kernel.RunLoop(kernel.Loop{
		Done:             func() bool { return a.life.Finished().Load() },
		StepLimitReached: r.stepLimitReached,
		Step:             func() bool { return r.runTurn(input, callback) },
		FinishStep:       func() { a.life.Finished().Store(true) },
	})
	result := r.finishRun(callback)
	// Flush any steering messages that arrived too late to be drained during
	// the run, so they are still recorded in the context instead of being lost.
	result.Error = errors.Join(result.Error, a.drainSteering())
	r.restoreInterruptedRun(beforeRun, result)
	r.restoreUnansweredRun(beforeRun, result)
	r.publishSummary(result, callback)
	return result
}

// interruptedMarker is appended as a hint-tagged user message when an
// interrupted run is kept in the context: the tool calls above already took
// effect on disk, so the model must know where the turn stopped instead of
// being blind to its own work.
const interruptedMarker = "The user interrupted the previous run. The tool calls and results above already took effect — files on disk may have been modified. Do not assume the run finished; ask the user how to proceed."

func (r *loopRunner) restoreInterruptedRun(before contextmgr.ManagerSnapshot, result *RunResult) {
	if result == nil || !result.Interrupted {
		return
	}
	a := r.agent
	after := a.deps.ctxMgr.Snapshot()
	if len(after.Messages) <= len(before.Messages) {
		return // nothing was appended
	}
	if assistantCount(after.Messages) == assistantCount(before.Messages) {
		// The turn produced nothing — roll back so a retry does not send the
		// user message twice.
		a.deps.ctxMgr.Restore(before)
		return
	}
	// The turn already executed tools or produced output. Those are facts
	// the next turn must see: the file system keeps those changes, and a
	// context rollback here would leave the model blind to its own work.
	// Keep everything and append a marker stating where the turn stopped.
	a.deps.ctxMgr.Add("user", interruptedMarker, types.MessageSourceHint)
}

// restoreUnansweredRun rolls back a run that ended without a single model
// response — a misconfigured model, a dead endpoint, or a failed compaction
// before the first call. Without the rollback the user message stays
// stranded in the context and the next attempt would send both copies.
// Runs that produced any assistant message (text or tool call) are kept.
func (r *loopRunner) restoreUnansweredRun(before contextmgr.ManagerSnapshot, result *RunResult) {
	if result == nil || result.Interrupted {
		return
	}
	a := r.agent
	after := a.deps.ctxMgr.Snapshot()
	if len(after.Messages) <= len(before.Messages) {
		return // nothing was appended
	}
	if assistantCount(after.Messages) > assistantCount(before.Messages) {
		return // the model answered; keep the turn
	}
	a.deps.ctxMgr.Restore(before)
}

func assistantCount(messages []types.Message) int {
	count := 0
	for _, m := range messages {
		if m.Role == "assistant" {
			count++
		}
	}
	return count
}

func (r *loopRunner) runTurn(input string, callback RunCallback) (finished bool) {
	a := r.agent
	msgCountBefore := a.deps.ctxMgr.Len()

	if err := a.turnRunner.prepareTurn(input); err != nil {
		a.run.err = err
		a.clearFinalState()
		return true
	}
	defer a.deps.ctxMgr.SetHints("")

	if a.turnRunner.interruptedBeforeReasoning(callback) {
		return true
	}

	reasoning := a.modelRunner.reason(input)
	if a.turnRunner.retryAfterInterruptedReasoning(reasoning, msgCountBefore) {
		return false
	}
	if a.run.stopReason == policy.StopInterrupted {
		return true
	}

	if len(reasoning.ToolCalls) > 0 {
		return a.turnRunner.handleToolCalls(reasoning.ToolCalls, reasoning, callback)
	}

	return a.turnRunner.handleText(reasoning, callback)
}

func (r *loopRunner) startRun(input string, images []types.MessageImage) {
	a := r.agent
	a.Reset()
	a.deps.ctxMgr.BeginModelTurn()
	// AddUser is exactly Add("user", input, "user") when images is empty,
	// so the single path covers both.
	a.deps.ctxMgr.AddUser(input, images)
}

func (r *loopRunner) applyUserSubmitHooks() {
	a := r.agent
	if a.deps.gov == nil {
		return
	}
	for _, h := range collectHints(a.deps.gov.OnUserSubmit()) {
		h := h
		a.injectHint(&h)
	}
}

func (r *loopRunner) logGovernanceSummary() {
	a := r.agent
	if a.deps.gov == nil {
		return
	}
	logger.Log("[GOVERNANCE] task complete: steps=%d, %s", a.run.step, a.deps.gov.Summary())
}

func (r *loopRunner) stepLimitReached() bool {
	a := r.agent
	if a.run.step < maxAgentSteps {
		return false
	}
	r.stepLimitHit = true
	a.run.stopReason = policy.StopCompleted
	a.clearFinalState()
	return true
}

func (r *loopRunner) publishSummary(result *RunResult, callback RunCallback) {
	if result == nil {
		return
	}
	reason := "completed"
	if r.stepLimitHit {
		reason = "step_limit"
	}
	if result.Error != nil {
		reason = "execution_error"
	}
	if result.Interrupted {
		reason = "cancelled"
	}
	result.StopReason = reason
	if callback != nil {
		callback(protocol.StepEvent{Action: protocol.StepActionRunSummary, Summary: &protocol.RunSummary{StepCount: result.Steps, StopReason: reason}})
	}
}

func (r *loopRunner) finishRun(callback RunCallback) *RunResult {
	a := r.agent
	if a.getCtx().Err() != nil || a.run.stopReason == policy.StopInterrupted {
		return &RunResult{FinalOutput: msgInterrupted, Steps: a.run.step, Interrupted: true}
	}
	if a.run.err != nil {
		return &RunResult{Error: a.run.err, Steps: a.run.step}
	}
	return &RunResult{FinalOutput: r.resolveFinalOutput(callback), Steps: a.run.step}
}

// resolveFinalOutput picks the run's answer by priority: the recorded final
// text, else the latest text as display fallback, else a synthesized summary.
func (r *loopRunner) resolveFinalOutput(callback RunCallback) string {
	a := r.agent
	if a.run.stopReason == policy.StopCompleted {
		if a.run.finalText != "" {
			// Persist the final answer if it hasn't been already. Recordable
			// chat turns set finalPersisted=true; policy-block paths leave it
			// false, so the text the user saw would otherwise be absent from
			// the saved session.
			if !a.run.finalPersisted {
				a.deps.ctxMgr.AddAssistant(types.Message{Role: "assistant", Content: a.run.finalText})
				a.run.finalPersisted = true
			}
			return a.run.finalText
		}
		if a.run.lastText != "" {
			return a.run.lastText
		}
	}
	output := a.modelRunner.synthesize()
	a.run.finalPersisted = true
	if callback != nil {
		callback(protocol.StepEvent{Action: protocol.StepActionChat, Output: output})
	}
	return output
}
