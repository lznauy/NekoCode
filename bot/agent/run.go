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
	Input             string
	exploreCascade    int
	filesModified     bool
	verifyInjected    bool
	needsVerification bool
	quota             budget.ToolQuota
	garbledCount      int
	// Repeated tool call detection: signature -> consecutive turn count.
	consecutiveCalls  map[string]int
	repeatedCallCount int
	repeatedCallName  string
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

	// 每轮配额计算 + hook 提示注入。
	a.ctxMgr.AutoCompactIfNeeded()
	state.quota = budget.ComputeQuota(a.ctxMgr.TokenUsage())
	a.injectPreTurnHints(state)

	// 用户中止 / 上下文取消。
	a.drainSteering()
	if a.getCtx().Err() != nil {
		a.stopReason = hooks.StopInterrupted
		a.lastText = "Interrupted"
		if callback != nil {
			callback("chat", "", "", "Interrupted")
		}
		return true
	}

	// LLM 推理（内含 AutoCompact）。
	reasoning := a.Reason(state)

	// 中断回滚：丢弃本轮消息，下轮重试。
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

	// 工具调用路径。
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

	// 文本响应路径（无工具调用）。
	return a.handleText(reasoning, state, callback)
}

func (a *Agent) injectPreTurnHints(state *stepState) {
	if a.hooks == nil {
		return
	}
	hs := &hooks.State{
		NeedsVerification: state.needsVerification,
		VerifyInjected:    state.verifyInjected,
		AllTasksDone:      a.ctxMgr.AllTasksDone(),
		QuotaHard:         state.quota.Hard,
		QuotaReadsLeft:    max(0, state.quota.MaxReads-state.quota.UsedReads),
		ExplorationScore:  a.exploration.Score,
		ExploreCascade:    state.exploreCascade,
		StepInput:         state.Input,
	}
	hints := a.hooks.EvaluateInject(hs)
	a.ctxMgr.SetHints(hooks.FormatHints(hints))
}

// handleText runs guardrails on text-only responses. Returns true when the agent should stop.
func (a *Agent) handleText(reasoning *ReasoningResult, state *stepState, callback RunCallback) (finished bool) {

	// Track garbled tool calls.
	if reasoning.GarbledToolCall {
		state.garbledCount++
		writeAgentLog("GarbledToolCall: count=%d", state.garbledCount)
	}


	// Evaluate stop hooks.
	if a.hooks != nil {
		hs := &hooks.State{
			NeedsVerification: state.needsVerification,
			VerifyInjected:    state.verifyInjected,
			AllTasksDone:      a.ctxMgr.AllTasksDone(),
			FilesModified:     state.filesModified,
			ActionIsChat:      reasoning.Action == ActionChat,
			GarbledToolCall:   reasoning.GarbledToolCall,
			GarbledCount:      state.garbledCount,
			ExplorationScore:  a.exploration.Score,
			OnFirstTurn:       a.step == 0,
			StepInput:         state.Input,
			RepeatedCallCount: state.repeatedCallCount,
			RepeatedCallName:  state.repeatedCallName,
		}

		// Stop hooks: check if the agent should terminate.
		if reason, stop := a.hooks.EvaluateStop(hs); stop {
			a.stopReason = reason
			return true
		}

		// Inject hooks: fire the first guardrail that triggers.
		if hints := a.hooks.EvaluateInject(hs); len(hints) > 0 {
			hint := hints[0]
			if hint.Type == "verification" && hint.Severity != "critical" {
				state.verifyInjected = true
			}
			a.ctxMgr.Add("user", "[System] "+hint.Content)
			return false
		}

		// All clear.
		if state.needsVerification && state.verifyInjected && reasoning.Action == ActionChat && a.ctxMgr.AllTasksDone() {
			state.needsVerification = false
		}
	}

	// Normal completion.
	a.stopReason = hooks.StopCompleted
		a.step++
	a.lastText = reasoning.ActionInput
	if reasoning.Action == ActionChat {
		a.ctxMgr.AddAssistantResponse(reasoning.ActionInput, a.lastReason)
	}
	callback(reasoning.Action.String(), "", "", reasoning.ActionInput)
	return true
}

// collectCalls 把 LLM 返回的 ToolCall 列表转成执行器需要的 ToolCallItem 列表。
func (a *Agent) collectCalls(reasoning *ReasoningResult) []tools.ToolCallItem {
	if len(reasoning.ToolCalls) > 0 {
		out := make([]tools.ToolCallItem, len(reasoning.ToolCalls))
		for i, tc := range reasoning.ToolCalls {
			out[i] = tools.ToolCallItem{ID: tc.ID, Name: tc.Name, Args: tc.Args}
		}
		return out
	}
	return nil
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
