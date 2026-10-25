package agent

import (
	"fmt"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/policy"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

// turnRunner orchestrates the lifecycle of a single turn: preparation,
// interruption handling, and dispatching the reasoning result to tool
// execution or text completion.
type turnRunner struct {
	agent *Agent
}

// policyBlockFinal is the default reason injected when a policy blocks the
// final answer.
const policyBlockFinal = "final answer blocked by policy"

// newTurnRunner creates the per-agent turn orchestrator.
func newTurnRunner(agent *Agent) *turnRunner {
	return &turnRunner{agent: agent}
}

// interruptedBeforeReasoning reports whether the run was interrupted before
// the LLM call starts; if so it marks the stop reason and notifies the callback.
func (r *turnRunner) interruptedBeforeReasoning(callback RunCallback) bool {
	a := r.agent
	if err := a.drainSteering(); err != nil {
		a.run.err = err
		a.clearFinalState()
		return true
	}
	if a.getCtx().Err() == nil {
		return false
	}
	a.run.stopReason = policy.StopInterrupted
	a.run.lastText = msgInterrupted
	if callback != nil {
		callback(protocol.StepEvent{Action: protocol.StepActionChat, Output: msgInterrupted})
	}
	return true
}

// retryAfterInterruptedReasoning handles an interrupted LLM response:
// it rolls back the partial output, drains steering messages, and signals
// the loop to retry — unless the agent is already finishing.
func (r *turnRunner) retryAfterInterruptedReasoning(reasoning *reasoningResult, msgCountBefore int) bool {
	a := r.agent
	if !reasoning.Interrupted {
		return false
	}
	if a.life.Finished().Load() {
		a.run.stopReason = policy.StopInterrupted
		return false
	}
	// Count interrupted responses toward the step limit to prevent
	// unbounded loops when the LLM repeatedly produces interrupted output.
	a.run.step++
	a.deps.ctxMgr.TruncateTo(msgCountBefore)
	if err := a.drainSteering(); err != nil {
		a.run.err = err
		a.clearFinalState()
		a.life.Finished().Store(true)
	}
	return true
}

// handleToolCalls executes the requested tool calls and feeds the results
// back into the context. Tool activity resets the consecutive-hint/failure
// counters and the policy-block gate. Returns true when the run should finish.
func (r *turnRunner) handleToolCalls(calls []core.ToolCallItem, reasoning *reasoningResult, callback RunCallback) bool {
	a := r.agent
	a.run.consecutiveHints = 0
	a.run.consecutiveFailures = 0
	a.gate.Reset()
	return a.toolRunner.executeAndFeedback(calls, reasoning.TextContent, callback)
}

// handleText processes a text-only response (no tool calls): it tracks
// consecutive LLM failures, runs Stop hooks, and — unless a hook took
// over — completes the run with the text as the final answer.
func (r *turnRunner) handleText(reasoning *reasoningResult, callback RunCallback) (finished bool) {
	a := r.agent
	if reasoning.IsError {
		a.run.consecutiveFailures++
		if a.run.consecutiveFailures >= maxConsecutiveFailures {
			a.run.step++
			a.run.stopReason = policy.StopCompleted
			a.clearFinalState()
			return true
		}
	} else {
		a.run.consecutiveFailures = 0
	}

	recordable := isRecordableText(reasoning)
	if handled, finished := r.applyStopHooks(reasoning, recordable, callback); handled {
		return finished
	}

	r.completeWithText(reasoning, recordable, callback)
	return true
}

// isRecordableText reports whether the response is a clean final chat text
// (non-error, non-garbled) that should be persisted to the context.
func isRecordableText(reasoning *reasoningResult) bool {
	return !reasoning.IsError && !reasoning.GarbledToolCall && reasoning.Action == actionChat
}

// completeWithText finishes the run with a text answer: it records the
// text as the final output and emits the corresponding step event.
func (r *turnRunner) completeWithText(reasoning *reasoningResult, recordable bool, callback RunCallback) {
	a := r.agent
	a.run.stopReason = policy.StopCompleted
	a.run.step++
	r.recordReasoningText(reasoning, recordable)
	if callback != nil {
		callback(protocol.StepEvent{Action: stepActionForReasoning(reasoning.Action), Output: reasoning.ActionInput})
	}
}

// recordReasoningText stores the response text as lastText always, and as
// finalText only when recordable — in which case it is also appended to the
// context immediately and marked persisted.
func (r *turnRunner) recordReasoningText(reasoning *reasoningResult, recordable bool) {
	a := r.agent
	a.run.lastText = reasoning.ActionInput
	if recordable {
		a.deps.ctxMgr.AddAssistant(types.Message{Role: "assistant", Content: reasoning.ActionInput,
			ReasoningContent: reasoning.ReasoningContent, ReasoningSignature: reasoning.ReasoningSignature})
		a.run.finalText = reasoning.ActionInput
		a.run.finalPersisted = true
	} else {
		a.run.finalText = ""
		a.run.finalPersisted = false
	}
}

// applyStopHooks exposes the final-answer intent to the hook registry
// and evaluates Stop hooks. It returns handled=true when a hook result
// took over the turn outcome (with finished indicating whether the run ends).
func (r *turnRunner) applyStopHooks(reasoning *reasoningResult, recordable bool, callback RunCallback) (handled, finished bool) {
	a := r.agent
	if a.deps.gov == nil {
		return false, false
	}
	var intent policy.FinalIntent
	if recordable {
		intent = policy.FinalIntentFinal
	} else {
		intent = finalIntentForReasoning(reasoning)
	}

	out := policy.Resolve(a.deps.gov.BeforeStop(policy.TurnResult{Intent: intent, Garbled: reasoning.GarbledToolCall, Text: reasoning.ActionInput}, a.getCtx()))
	return r.applyStopOutcome(out, reasoning, recordable, callback)

}

// finalIntentForReasoning maps a non-recordable response to the final-intent
// label reported to Stop hooks.
func finalIntentForReasoning(reasoning *reasoningResult) policy.FinalIntent {
	switch {
	case reasoning.IsError:
		return policy.FinalIntentError
	case reasoning.GarbledToolCall:
		return policy.FinalIntentFormatError
	default:
		return policy.FinalIntentNonFinal
	}
}

// applyStopHint injects a Stop hint so the model revises its answer
// on the next turn. It finishes the run instead once the consecutive-hint
// ceiling is reached, preventing a text-only hint loop.
func (r *turnRunner) applyStopHint(reasoning *reasoningResult, hint *policy.Hint, recordable bool, callback RunCallback) bool {
	a := r.agent
	a.run.consecutiveHints++
	if a.run.consecutiveHints >= maxConsecutiveHints {
		a.run.step++
		a.run.stopReason = policy.StopCompleted
		if reasoning.IsError || reasoning.GarbledToolCall {
			a.clearFinalState()
		} else {
			r.recordReasoningText(reasoning, recordable)
		}
		return true
	}
	r.recordReasoningText(reasoning, recordable)
	if recordable {
		if callback != nil {
			callback(protocol.StepEvent{Action: stepActionForReasoning(reasoning.Action), Output: reasoning.ActionInput})
		}
	}
	a.injectHint(hint)
	a.run.step++
	return false
}

// stepActionForReasoning maps a reasoning action to the UI step action
// used in step events.
func stepActionForReasoning(action actionType) protocol.StepAction {
	switch action {
	case actionChat:
		return protocol.StepActionChat
	case actionExecuteTool:
		return protocol.StepActionExecuteTool
	default:
		return protocol.StepAction("")
	}
}

// A rejected answer is never committed as an accepted final response.
func (r *turnRunner) applyFinalPolicyBlock(reasoning *reasoningResult, reason string) bool {
	a := r.agent
	if reason == "" {
		reason = policyBlockFinal
	}
	a.stream.discardText()
	a.clearFinalState()
	a.run.lastText = ""
	if !a.gate.Try() {
		a.run.stopReason = policy.StopPolicyBlocked
		a.run.err = fmt.Errorf("final answer rejected after retries: %s", reason)
		return false
	}
	a.injectHint(&policy.Hint{Type: "policy_block", Severity: "critical", Content: reason})
	a.run.step++
	return true
}

// applyPostToolHookResult applies a post-tool hook result (called from the
// toolRunner after tool execution): Stop ends the run, RequireTool
// and BlockFinal inject critical hints, and a plain Hint is queued for the
// next turn. Returns true when the run should finish.
func (a *Agent) applyPostToolHookResult(result policy.Result) bool {
	return a.toolRunner.applyPolicyResults([]policy.Result{result})
}

func (r *turnRunner) applyStopHookResult(result policy.Result, reasoning *reasoningResult, recordable bool, callback RunCallback) (bool, bool) {
	return r.applyStopOutcome(policy.Resolve([]policy.Result{result}), reasoning, recordable, callback)
}

func (r *turnRunner) applyStopOutcome(out policy.Outcome, reasoning *reasoningResult, recordable bool, callback RunCallback) (bool, bool) {
	a := r.agent
	switch out.Action {
	case policy.Terminate:
		a.run.hookStopped = true
		a.run.stopReason = out.StopReason
		if out.StopReason == policy.StopPolicyBlocked {
			a.stream.discardText()
			a.run.err = fmt.Errorf("hook stopped run: %s", out.Reason)
			a.clearFinalState()
		} else {
			r.recordReasoningText(reasoning, recordable)
		}
		return true, true
	case policy.Reject:
		for i := range out.Hints {
			a.injectHint(&out.Hints[i])
		}
		return true, !r.applyFinalPolicyBlock(reasoning, out.Reason)
	}
	if len(out.Hints) > 0 {
		// One retry for the entire hint set, preserving every hook's guidance.
		for i := 1; i < len(out.Hints); i++ {
			a.injectHint(&out.Hints[i])
		}
		return true, r.applyStopHint(reasoning, &out.Hints[0], recordable, callback)
	}
	return false, false
}
