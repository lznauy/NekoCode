package agent

import (
	"fmt"
	"sort"
	"strings"

	"nekocode/bot/agent/budget"
	"nekocode/bot/hooks"
	"nekocode/bot/tools"
)

type RunResult struct {
	FinalOutput string
	Error       error
	Steps       int
}

type stepState struct {
	Input string
	quota budget.ToolQuota
}

type RunCallback func(action, toolName, toolArgs, output string)

func (a *Agent) Run(input string, callback RunCallback) *RunResult {
	a.Reset()
	a.ctxMgr.Add("user", input)
	state := &stepState{Input: input}

	for !a.finished {
		if finished := a.runTurn(state, callback); finished {
			a.finished = true
		}
	}

	if a.getCtx().Err() != nil || a.stopReason == hooks.StopInterrupted {
		return &RunResult{FinalOutput: "Interrupted", Steps: a.step}
	}
	if a.stopReason == hooks.StopCompleted && a.lastText != "" {
		return &RunResult{FinalOutput: a.lastText, Steps: a.step}
	}
	return a.synthesizeAndReturn(callback)
}

func (a *Agent) runTurn(state *stepState, callback RunCallback) (finished bool) {
	msgCountBefore := a.ctxMgr.Len()

	a.ctxMgr.AutoCompactIfNeeded()
	state.quota = budget.ComputeQuota(a.ctxMgr.TokenUsage())

	// —— PreTurn hooks ——
	if a.hookMgr != nil {
		a.hookMgr.Gauge(hooks.KeyQuotaHard, b2i(state.quota.Hard))
		a.hookMgr.Gauge(hooks.KeyQuotaReads, int64(max(0, state.quota.MaxReads-state.quota.UsedReads)))
		a.hookMgr.Gauge(hooks.KeyExploreScore, int64(a.exploration.Score))
		a.hookMgr.Gauge(hooks.KeyTasksAllDone, b2i(a.ctxMgr.AllTasksDone()))
		a.hookMgr.Value(hooks.KeyStepInput, state.Input)
		a.hookMgr.ResetTurn()

		var hints []hooks.Hint
		for _, r := range a.hookMgr.Evaluate(hooks.PointPreTurn) {
			if r.Hint != nil {
				hints = append(hints, *r.Hint)
			}
		}
		a.ctxMgr.SetHints(hooks.FormatHints(hints))
	}

	a.drainSteering()
	if a.getCtx().Err() != nil {
		a.stopReason = hooks.StopInterrupted
		a.lastText = "Interrupted"
		if callback != nil {
			callback("chat", "", "", "Interrupted")
		}
		return true
	}

	reasoning := a.Reason(state)

	if reasoning.Interrupted {
		state.quota.Rollback(state.quota.Snapshot())
		if a.finished {
			a.stopReason = hooks.StopInterrupted
			return true
		}
		a.ctxMgr.TruncateTo(msgCountBefore)
		a.drainSteering()
		return false
	}

	calls := a.collectCalls(reasoning)
	if len(calls) > 0 {
		var stopReason hooks.StopReason
		var shouldStop bool
		state, shouldStop, stopReason = a.executeAndFeedback(calls, reasoning, state, callback)
		if shouldStop {
			a.stopReason = stopReason
			return true
		}
		return false
	}

	return a.handleText(reasoning, state, callback)
}

func (a *Agent) handleText(reasoning *ReasoningResult, state *stepState, callback RunCallback) (finished bool) {
	if a.hookMgr != nil {
		if reasoning.GarbledToolCall {
			a.hookMgr.Counter(hooks.KeyRespGarbled)
		}
		if reasoning.Action == ActionChat {
			a.hookMgr.Turn(hooks.KeyRespChat)
		}

		// —— PostTurn hooks ——
		results := a.hookMgr.Evaluate(hooks.PointPostTurn)
		for _, r := range results {
			if r.Stop != nil {
				a.stopReason = *r.Stop
				return true
			}
			if r.Hint != nil {
				a.ctxMgr.Add("user", "[System] "+r.Hint.Content)
				return false
			}
		}
	}

	a.stopReason = hooks.StopCompleted
	a.step++
	a.lastText = reasoning.ActionInput
	if reasoning.Action == ActionChat {
		a.ctxMgr.AddAssistantResponse(reasoning.ActionInput, a.lastReason)
	}
	callback(reasoning.Action.String(), "", "", reasoning.ActionInput)
	return true
}

func b2i(b bool) int64 {
	if b { return 1 }
	return 0
}

// collectCalls returns tool calls for execution.
func (a *Agent) collectCalls(reasoning *ReasoningResult) []tools.ToolCallItem {
	return reasoning.ToolCalls
}

// synthesizeAndReturn 额外调一次 LLM 生成总结，用于非正常结束的兜底输出。
func (a *Agent) synthesizeAndReturn(callback RunCallback) *RunResult {
	output := a.forceSynthesize()
	a.ctxMgr.AddAssistantResponse(output, "")
	if callback != nil {
		callback("chat", "", "", output)
	}
	return &RunResult{FinalOutput: output, Steps: a.step}
}

// drainSteering 清空 steering 通道中积压的用户中途消息。
func (a *Agent) drainSteering() {
	for {
		select {
		case msg := <-a.steering:
			a.ctxMgr.Add("user", msg)
		default:
			return
		}
	}
}

// extractFilePath 从工具调用参数中提取文件路径。
func extractFilePath(tc tools.ToolCallItem) string {
	if path, ok := tc.Args["path"].(string); ok {
		return path
	}
	if path, ok := tc.Args["filePath"].(string); ok {
		return path
	}
	return ""
}

// formatArgs 将工具参数格式化为 key=value 字符串供 TUI 展示。
func formatArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var pairs []string
	for _, k := range keys {
		val := fmt.Sprint(args[k])
		if strings.ContainsAny(val, ",=\"") {
			val = `"` + strings.ReplaceAll(strings.ReplaceAll(val, "\\", "\\\\"), "\"", "\\\"") + `"`
		}
		pairs = append(pairs, k+"="+val)
	}
	return strings.Join(pairs, ",")
}
