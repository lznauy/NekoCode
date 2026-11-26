package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"nekocode/bot/agent/internal/kernel"
	"nekocode/bot/contextmgr"
	"nekocode/bot/policy"
	"nekocode/bot/provider/types"
	"nekocode/logger"
	"nekocode/protocol"
	"nekocode/util/modelbudget"
)

// maxAgentSteps is the hard ceiling on agent loop iterations. Prevents
// infinite loops when the LLM keeps producing tool calls or Stop hooks
// keep injecting hints without progress.
const maxAgentSteps = 150

// maxConsecutiveHints is the maximum number of consecutive Stop hint
// injections without any intervening tool calls. Beyond this the agent
// is stuck in a text-only loop and should stop.
const maxConsecutiveHints = 3

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

func (r *loopRunner) runContext(ctx context.Context, input string, images []types.MessageImage, callback RunCallback) *RunResult {
	a := r.agent
	r.stepLimitHit = false
	a.life.SetRunContext(ctx)
	defer a.life.SetRunContext(nil)
	if err := a.life.Context().Err(); err != nil {
		return &RunResult{Error: err, Interrupted: true}
	}
	beforeRun := a.deps.ctxMgr.Snapshot()
	err := r.startRun(input, images)
	defer a.life.StopTiming()
	if err != nil {
		result := &RunResult{Error: err}
		r.publishSummary(result, callback)
		return result
	}
	defer r.logGovernanceSummary()
	if a.deps.gov != nil {
		r.applyEntryOutcome(policy.Resolve(a.deps.gov.StartRun(a.getCtx(), input)))
		defer func() {
			intent := policy.FinalIntentFinal
			if a.run.err != nil || a.getCtx().Err() != nil {
				intent = policy.FinalIntentError
			}
			a.deps.gov.EndRun(a.getCtx(), policy.TurnResult{Intent: intent, Text: a.run.finalText})
		}()
	}
	if a.life.Context().Err() == nil && !a.life.Finished().Load() {
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
	result.Error = errors.Join(result.Error, a.drainSteering(), a.run.evidenceErr)
	r.settleTurnHistory(beforeRun, result)
	a.life.StopTiming()
	r.publishSummary(result, callback)
	return result
}

// interruptedMarker is appended as a hint-tagged user message when an
// interrupted run is kept in the context: the tool calls above already took
// effect on disk, so the model must know where the turn stopped instead of
// being blind to its own work.
const interruptedMarker = "The user interrupted the previous run. The tool calls and results above already took effect — files on disk may have been modified. Do not assume the run finished; ask the user how to proceed."

// The transcript sequence survives compaction and acknowledgement. A shorter
// active context can still contain a new turn and its tool side effects.
func (r *loopRunner) settleTurnHistory(before contextmgr.ManagerSnapshot, result *RunResult) {
	manager := r.agent.deps.ctxMgr
	base, end, assistant := manager.TranscriptPosition()
	start := before.TranscriptBase + len(before.Transcript)
	if end <= start {
		return
	}
	if assistant > start {
		if result.Interrupted {
			manager.Add("user", interruptedMarker, types.MessageSourceHint)
		}
		return
	}
	// Pre-compaction backup can already have committed some or all of the
	// transcript. Roll back working context without rewriting durable records.
	if base > before.TranscriptBase {
		acknowledged := min(base-before.TranscriptBase, len(before.Transcript))
		before.Transcript = before.Transcript[acknowledged:]
		before.TranscriptBase = base
		before.TranscriptLazy = true
	}
	manager.Restore(before)
}

func (r *loopRunner) runTurn(input string, callback RunCallback) (finished bool) {
	a := r.agent

	a.applyTurnHints(nil)
	defer a.deps.ctxMgr.SetHints("")

	if a.turnRunner.interruptedBeforeReasoning(callback) {
		return true
	}

	reasoning := a.modelRunner.reason(input)
	if a.turnRunner.retryAfterInterruptedReasoning(reasoning) {
		return false
	}
	if a.run.stopReason == policy.StopInterrupted {
		return true
	}
	if a.run.err != nil {
		a.clearFinalState()
		return true
	}

	if reasoning.Action == actionEmptyResponse {
		a.clearFinalState()
		if a.run.hasToolResults {
			// finishRun will summarize the tool results without running more tools.
			a.run.partialCloseout = true
			a.run.step++
		} else {
			a.run.err = fmt.Errorf("%w: model returned no visible text or tool calls", protocol.ErrIncompleteResponse)
		}
		return true
	}

	if len(reasoning.ToolCalls) > 0 {
		return a.turnRunner.handleToolCalls(reasoning.ToolCalls, reasoning, callback)
	}

	return a.turnRunner.handleText(reasoning, callback)
}

func (r *loopRunner) startRun(input string, images []types.MessageImage) error {
	a := r.agent
	a.Reset()
	runtimeInput := protocol.CoordinationFromContext(a.getCtx()).Notification || protocol.IsGoalContinuation(a.getCtx())
	preservationInput := input
	if runtimeInput {
		preservationInput = ""
	}
	if goal, ok := a.getCtx().Value(goalPreservationKey{}).(*GoalPreservation); ok {
		a.run.preserved = goal.Files
		preservationInput = goal.Criteria + "\n" + preservationInput
	}
	a.run.identity = protocol.ExecutionFromContext(a.life.Context())
	if a.run.identity.RunID == "" {
		a.run.identity.RunID = uuid.NewString()
	}
	group := protocol.CoordinationFromContext(a.getCtx()).GroupID
	if group == "" {
		group = a.run.identity.RunID
	}
	if err := a.capturePreservedFiles(preservationInput); err != nil {
		return err
	}
	a.deps.ctxMgr.SelectTodoGroup(group)
	a.deps.ctxMgr.BeginModelTurn()
	if runtimeInput {
		a.deps.ctxMgr.Add("user", input, types.MessageSourceRuntimeEvent)
	} else {
		a.deps.ctxMgr.AddUser(input, images)
	}
	return nil
}

func (r *loopRunner) applyUserSubmitHooks() {
	a := r.agent
	if a.deps.gov == nil {
		return
	}
	r.applyEntryOutcome(policy.Resolve(a.deps.gov.OnUserSubmit(a.getCtx())))
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
	if a := r.agent; a.run.partialCloseout {
		reason = "incomplete"
	}
	if result.Error != nil {
		reason = "execution_error"
		if protocol.OnlyFailure(result.Error, modelbudget.ErrExhausted) {
			reason = "call_budget"
		} else if protocol.OnlyIncompleteResponse(result.Error) {
			reason = "incomplete"
		}
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
	if a.run.hookStopped && a.run.finalText == "" {
		return &RunResult{Steps: a.run.step}
	}
	output := r.resolveFinalOutput(callback)
	if a.getCtx().Err() != nil || a.run.stopReason == policy.StopInterrupted {
		return &RunResult{FinalOutput: msgInterrupted, Steps: a.run.step, Interrupted: true}
	}
	if a.run.err != nil {
		return &RunResult{Error: a.run.err, Steps: a.run.step}
	}
	return &RunResult{FinalOutput: output, Steps: a.run.step}
}

// resolveFinalOutput picks the run's answer by priority: the recorded final
// text, else the latest text as display fallback, else a synthesized summary.
func (r *loopRunner) resolveFinalOutput(callback RunCallback) string {
	a := r.agent
	if a.run.stopReason == policy.StopCompleted {
		if a.run.finalText != "" {
			// Persist an accepted answer if its producing path has not already
			// appended it. Rejected candidates are never stored in finalText.
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
	if a.run.err != nil || a.run.stopReason == policy.StopInterrupted || a.getCtx().Err() != nil {
		return ""
	}
	if callback != nil {
		callback(protocol.StepEvent{Action: protocol.StepActionChat, Output: output})
	}
	return output
}

func (r *loopRunner) applyEntryOutcome(out policy.Outcome) {
	for i := range out.Hints {
		r.agent.injectHint(&out.Hints[i])
	}
	if out.Action != policy.Continue {
		r.agent.run.err = fmt.Errorf("hook stopped run: %s", out.Reason)
		r.agent.run.stopReason = policy.StopPolicyBlocked
		r.agent.clearFinalState()
		r.agent.life.Finished().Store(true)
	}
}
