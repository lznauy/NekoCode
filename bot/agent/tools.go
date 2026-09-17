package agent

import (
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/google/uuid"

	"nekocode/bot/agent/internal/toolpolicy"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/policy"
	"nekocode/bot/provider/types"
	"nekocode/logger"
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
		msgs[i] = types.Message{Content: content, ToolCallID: r.ID, IsError: r.Error != ""}
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
		executor.SetPreviewFn(func(callID, toolName string, _ map[string]any, preview string) {
			callback(protocol.StepEvent{Action: protocol.StepActionToolPreview, CallID: callID, ToolName: toolName, Output: preview})
		})
	} else {
		executor.SetPreviewFn(nil)
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
		name, _ := calls[i].Effective()
		toolResults[i] = ctxmgr.ToolResultMsg{Message: m, ToolName: name}
	}
	r.agent.deps.ctxMgr.AddToolResultsBatch(toolResults)

	for _, h := range preToolHints {
		r.agent.injectHint(h)
	}
}

type subSlotInfo struct {
	subID string
	start func(protocol.StepEvent)
}

var subSlotFullReason = fmt.Sprintf("task not started: subagent slot pool is full (%d/%d); retry after the current batch finishes", maxSubSlots, maxSubSlots)

// prepareSubagentCallbacks reserves a sub-agent slot for each "task" call and
// wires per-sub-agent event forwarding. When the slot pool is full (more than
// maxSubSlots tasks in one batch) overflow tasks are denied fast — removed from
// the returned allowed slice and reported in the denied map keyed by original
// call index — instead of blocking the main goroutine until the batch finishes.
func (r *toolRunner) prepareSubagentCallbacks(allowed []core.ToolCallItem, allowedIdx []int, callback RunCallback) (func(), map[int]string, []core.ToolCallItem) {
	denied := make(map[int]string)
	kept := make([]core.ToolCallItem, 0, len(allowed))
	var taskInfos []subSlotInfo
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
		colorIdx, ok := r.agent.deps.subSlotMgr.Acquire(subID, subProfile)
		if !ok {
			logger.Log("subSlotMgr: Acquire failed for %s (all slots full)", subProfile)
			denied[allowedIdx[i]] = subSlotFullReason
			continue
		}
		sid := subID
		cid := colorIdx
		fallback := protocol.StepEvent{
			Action:       protocol.StepActionSubAgentStart,
			SubAgentType: subProfile, SubAgentProfile: subProfile,
			SubAgentSkills: stringSliceValue(c.Args["skills"]),
		}
		var started sync.Once
		// Prefer resolved runner metadata. Retain a paired lifecycle for tasks
		// that fail validation or runners that only forward tool events.
		start := func(ev protocol.StepEvent) {
			started.Do(func() {
				if ev.Action != protocol.StepActionSubAgentStart {
					ev = fallback
				}
				ev.SubAgentID, ev.SubAgentColor = sid, cid
				if callback != nil {
					callback(ev)
				}
			})
		}
		taskInfos = append(taskInfos, subSlotInfo{sid, start})
		c.Args["_sub_callback"] = taskbridge.TaskCallback{ID: sid, Callback: func(ev protocol.StepEvent) {
			start(ev)
			if ev.Action == protocol.StepActionSubAgentStart {
				return
			}
			if callback == nil {
				return
			}
			ev.SubAgentID = sid
			ev.SubAgentColor = cid
			callback(ev)
		}}
		kept = append(kept, c)
	}

	return func() {
		for _, ti := range taskInfos {
			ti.start(protocol.StepEvent{})
			r.agent.deps.subSlotMgr.Release(ti.subID)
			if callback != nil {
				callback(protocol.StepEvent{
					Action:     protocol.StepActionSubAgentEnd,
					SubAgentID: ti.subID,
				})
			}
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
