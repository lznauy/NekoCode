package agent

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	"nekocode/bot/agent/internal/toolpolicy"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/policy"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

// toolRunner orchestrates tool execution within a turn: filtering, execution,
// result feedback, and post-tool hooks.
type toolRunner struct {
	agent *Agent
}

func newToolRunner(agent *Agent) *toolRunner {
	return &toolRunner{agent: agent}
}

func (r *toolRunner) executeAndFeedback(calls []core.ToolCallItem, textContent string, callback RunCallback) bool {
	if textContent != "" && callback != nil {
		callback(protocol.StepEvent{Action: protocol.StepActionThink, Output: textContent})
	}

	for i := range calls {
		calls[i] = r.agent.deps.toolRegistry.EnrichCall(calls[i])
	}
	filtered := r.filterToolCalls(calls)

	cleanupSubagents, deniedSubagents, kept := r.prepareSubagentCallbacks(filtered.Allowed, filtered.AllowedIdx, callback)
	filtered.Allowed = kept
	maps.Copy(filtered.Blocked, deniedSubagents)
	defer cleanupSubagents()

	r.agent.deps.toolExecutor.PreparePreviewsContext(r.agent.getCtx(), filtered.Allowed)
	emitStartCallbacks(calls, filtered.Blocked, callback)

	execResults := r.executeAllowedTools(filtered.Allowed, callback)
	results := mergeResults(calls, filtered.Blocked, execResults)
	policyResults := r.recordToolResults(calls, filtered.Blocked, results)

	msgs := emitResultCallbacks(calls, filtered.Blocked, results, callback)
	r.addToolResultsAndHints(calls, msgs, filtered.PreToolHints)

	if r.applyPolicyResults(policyResults) {
		return true
	}
	r.agent.run.step++
	return false
}

func (r *toolRunner) applyPolicyResults(results []policy.Result) bool {
	return slices.ContainsFunc(results, r.agent.applyPostToolHookResult)
}

type filteredCalls struct {
	Allowed      []core.ToolCallItem
	AllowedIdx   []int // original index in the caller's calls slice, parallel to Allowed
	Blocked      map[int]string
	PreToolHints []*policy.Hint
}

func (r *toolRunner) filterToolCalls(calls []core.ToolCallItem) filteredCalls {
	out := filteredCalls{
		Allowed: make([]core.ToolCallItem, 0, len(calls)),
		Blocked: make(map[int]string),
	}
	for i, c := range calls {
		if r.applyPreToolPolicy(effectiveToolCall(c), out.Blocked, i, &out.PreToolHints) {
			continue
		}

		out.Allowed = append(out.Allowed, c)
		out.AllowedIdx = append(out.AllowedIdx, i)
	}
	return out
}

func effectiveToolCall(call core.ToolCallItem) core.ToolCallItem {
	name, args := call.Effective()
	call.Name = name
	call.Args = args
	return call
}

func (r *toolRunner) applyPreToolPolicy(c core.ToolCallItem, blocked map[int]string, idx int, hints *[]*policy.Hint) bool {
	decision := toolpolicy.Check(r.agent.deps.gov, c)
	for i := range decision.Hints {
		hint := decision.Hints[i]
		*hints = append(*hints, &hint)
	}
	if decision.BlockReason != "" {
		blocked[idx] = decision.BlockReason
		return true
	}
	return false
}

func emitStartCallbacks(calls []core.ToolCallItem, blocked map[int]string, callback RunCallback) {
	if callback == nil {
		return
	}
	for i, c := range calls {
		c = effectiveToolCall(c)
		action := protocol.StepActionToolStart
		preview, _ := c.Args["_preview"].(string)
		if reason, ok := blocked[i]; ok {
			action = protocol.StepActionToolBlocked
			preview = reason
		}
		callback(protocol.StepEvent{Action: action, CallID: c.ID, ToolName: c.Name, ToolArgs: core.FormatArgs(c.Args), ToolInput: core.JSONArgs(c.Args), Output: preview})
	}
}

func mergeResults(calls []core.ToolCallItem, blocked map[int]string, execResults []core.ToolCallResult) []core.ToolCallResult {
	results := make([]core.ToolCallResult, len(calls))
	execIdx := 0
	for i := range calls {
		if msg, ok := blocked[i]; ok {
			name, _ := calls[i].Effective()
			results[i] = core.ToolCallResult{ID: calls[i].ID, Name: name, Error: msg}
			continue
		}
		results[i] = execResults[execIdx]
		execIdx++
	}
	return results
}

func emitResultCallbacks(calls []core.ToolCallItem, blocked map[int]string, results []core.ToolCallResult, callback RunCallback) []types.Message {
	msgs := make([]types.Message, len(results))
	for i, r := range results {
		call := effectiveToolCall(calls[i])
		content := r.EffectiveOutput()
		msgs[i] = types.Message{Content: r.TextOutput().Text, ToolOutput: r.Artifact, ToolCallID: r.ID, IsError: r.Error != ""}
		if callback != nil {
			if _, isBlocked := blocked[i]; isBlocked {
				continue
			}
			callback(protocol.StepEvent{Action: protocol.StepActionExecuteTool, CallID: r.ID, ToolName: r.Name, ToolArgs: core.FormatArgs(call.Args), ToolInput: core.JSONArgs(call.Args), Output: content, IsError: r.Error != ""})
		}
	}
	return msgs
}

func (r *toolRunner) executeAllowedTools(allowed []core.ToolCallItem, callback RunCallback) []core.ToolCallResult {
	executor := r.agent.deps.toolExecutor
	if callback != nil {
		executor.SetPreviewDecisionFn(func(callID, toolName string, _ map[string]any, preview string, decision protocol.ToolDecision) {
			callback(protocol.StepEvent{Action: protocol.StepActionToolPreview, CallID: callID, ToolName: toolName, Output: preview, Decision: decision})
		})
	} else {
		executor.SetPreviewDecisionFn(nil)
	}
	if len(allowed) == 0 {
		return nil
	}
	return executor.ExecuteBatch(r.agent.getCtx(), allowed)
}

func (r *toolRunner) recordToolResults(calls []core.ToolCallItem, blocked map[int]string, results []core.ToolCallResult) []policy.Result {
	gov := r.agent.deps.gov
	if gov == nil {
		return nil
	}
	events := make([]policy.ToolResult, 0, len(calls))
	for i, tc := range calls {
		tc = effectiveToolCall(tc)
		if msg, ok := blocked[i]; ok {
			events = append(events, policy.ToolResult{
				Name:        tc.Name,
				Args:        tc.Args,
				Blocked:     true,
				BlockReason: msg,
			})
			continue
		}
		events = append(events, policy.ToolResult{
			Name:   tc.Name,
			Args:   tc.Args,
			Output: results[i].Output,
			Error:  results[i].Error,
		})
	}
	return gov.RecordTools(events)
}

func (r *toolRunner) addToolResultsAndHints(calls []core.ToolCallItem, msgs []types.Message, preToolHints []*policy.Hint) {
	toolResults := make([]ctxmgr.ToolResultMsg, len(msgs))
	for i, m := range msgs {
		name, args := calls[i].Effective()
		if name == "skill" && !m.IsError {
			m.SkillName, _ = args["name"].(string)
		}
		toolResults[i] = ctxmgr.ToolResultMsg{Message: m, ToolName: name, AllowOutputFile: r.agent.deps.toolExecutor.AllowOutputFile}
	}
	r.agent.deps.ctxMgr.AddToolResultsBatch(toolResults)

	for _, h := range preToolHints {
		r.agent.injectHint(h)
	}
}

// Reserve task identities before execution; workers acquire color slots only
// when admitted. Queued tasks remain visible without occupying execution slots.
func (r *toolRunner) prepareSubagentCallbacks(allowed []core.ToolCallItem, allowedIdx []int, callback RunCallback) (func(), map[int]string, []core.ToolCallItem) {
	denied := make(map[int]string)
	kept := make([]core.ToolCallItem, 0, len(allowed))
	var finishTasks []func(protocol.StepEvent)
	for i, c := range allowed {
		if c.Name != "task" {
			kept = append(kept, c)
			continue
		}
		subProfile, _ := c.Args["profile"].(string)
		if subProfile == "" {
			subProfile = "coder"
		}
		subID := uuid.New().String()
		resumeID, _ := c.Args["resume_id"].(string)
		if resumeID != "" {
			subID = resumeID
		}
		if !r.agent.deps.subSlotMgr.Reserve(subID) {
			denied[allowedIdx[i]] = "task not started: saved task is already queued or running; retry after it finishes"
			continue
		}
		sid := subID
		var attemptMu sync.RWMutex
		var attemptID string
		var attemptSeq int
		var cid atomic.Int64
		cid.Store(-1)
		fallback := protocol.StepEvent{
			Action:       protocol.StepActionSubAgentStart,
			SubAgentType: subProfile, SubAgentProfile: subProfile,
			SubAgentSkills: stringSliceValue(c.Args["skills"]),
			SubAgentPrompt: stringArgValue(c.Args["prompt"]),
			SubAgentStatus: "running",
		}
		if callback != nil {
			queued := fallback
			queued.SubAgentID, queued.SubAgentColor, queued.SubAgentStatus = sid, -1, "queued"
			callback(queued)
		}
		var started sync.Once
		var didStart atomic.Bool
		// Prefer resolved runner metadata. Retain a paired lifecycle for tasks
		// that fail validation or runners that only forward tool events.
		start := func(ev protocol.StepEvent) {
			attemptMu.Lock()
			if ev.SubAgentAttemptID != "" {
				attemptID, attemptSeq = ev.SubAgentAttemptID, ev.SubAgentAttemptSeq
			}
			attemptMu.Unlock()
			started.Do(func() {
				didStart.Store(true)
				if ev.Action != protocol.StepActionSubAgentStart {
					ev = fallback
				}
				ev.SubAgentID, ev.SubAgentColor = sid, int(cid.Load())
				attemptMu.RLock()
				ev.SubAgentAttemptID, ev.SubAgentAttemptSeq = attemptID, attemptSeq
				attemptMu.RUnlock()
				if ev.SubAgentPrompt == "" {
					ev.SubAgentPrompt = fallback.SubAgentPrompt
				}
				if ev.SubAgentStatus == "" {
					ev.SubAgentStatus = "running"
				}
				if callback != nil {
					callback(ev)
				}
			})
		}
		var ended sync.Once
		finish := func(ev protocol.StepEvent) {
			if ev.SubAgentAttemptID == "" {
				attemptMu.RLock()
				ev.SubAgentAttemptID, ev.SubAgentAttemptSeq = attemptID, attemptSeq
				attemptMu.RUnlock()
			}
			ended.Do(func() {
				r.agent.deps.subSlotMgr.Release(sid)
				// A rejected resume must not overwrite the saved task's state.
				if resumeID != "" && !didStart.Load() {
					if callback != nil {
						callback(protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentID: sid, SubAgentColor: -1, SubAgentStatus: "not_started"})
					}
					return
				}
				// Cancellation before admission has no running lifecycle.
				if cid.Load() >= 0 {
					start(protocol.StepEvent{})
				}
				ev.Action, ev.SubAgentID, ev.SubAgentColor = protocol.StepActionSubAgentEnd, sid, int(cid.Load())
				if ev.SubAgentStatus == "" {
					ev.SubAgentStatus = "interrupted"
				}
				if callback != nil {
					callback(ev)
				}
			})
		}
		finishTasks = append(finishTasks, finish)
		// Execution hooks must never mutate model arguments retained by audit
		// records, including when validation or admission fails before execution.
		c.Args = maps.Clone(c.Args)
		if c.Args == nil {
			c.Args = make(map[string]any)
		}
		c.Args["_sub_callback"] = taskbridge.TaskCallback{ID: sid, Finish: finish, Begin: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			color, ok := r.agent.deps.subSlotMgr.Acquire(sid, subProfile)
			if !ok {
				return fmt.Errorf("task execution slot unavailable")
			}
			cid.Store(int64(color))
			return nil
		}, Callback: func(ev protocol.StepEvent) {
			if ev.Action == protocol.StepActionSubAgentEnd {
				finish(ev)
				return
			}
			start(ev)
			if ev.Action == protocol.StepActionSubAgentStart {
				return
			}
			if callback == nil {
				return
			}
			ev.SubAgentID = sid
			ev.SubAgentColor = int(cid.Load())
			if ev.SubAgentAttemptID == "" {
				attemptMu.RLock()
				ev.SubAgentAttemptID, ev.SubAgentAttemptSeq = attemptID, attemptSeq
				attemptMu.RUnlock()
			}
			callback(ev)
		}}
		kept = append(kept, c)
	}

	return func() {
		for _, finish := range finishTasks {
			finish(protocol.StepEvent{})
		}
	}, denied, kept
}

func stringSliceValue(value any) []string {
	switch items := value.(type) {
	case []string:
		return append([]string(nil), items...)
	case []any:
		result := make([]string, 0, len(items))
		for _, item := range items {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func stringArgValue(v any) string { s, _ := v.(string); return s }
