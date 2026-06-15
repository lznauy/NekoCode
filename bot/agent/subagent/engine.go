package subagent

import (
	"context"
	"strings"
	"time"

	"nekocode/bot/ctxmgr"
	ctxfmt "nekocode/bot/ctxmgr/context"
	"nekocode/bot/debug"
	"nekocode/bot/tools"
	"nekocode/llm/types"
	"nekocode/llm"

	"nekocode/common"
)

const (
	thoroughDeep    = "very thorough"
	taskToolName    = "task"
	maxSubAgentSteps = 50
)

// Engine runs a sub-agent loop. Fully self-contained — does not import agent.
type Engine struct {
	llmClient    types.LLM
	toolRegistry *tools.Registry
	executor     *tools.Executor
	mergeClient  types.LLM
}

func NewEngine(llmClient types.LLM, registry *tools.Registry, mergeClient types.LLM) *Engine {
	e := tools.NewExecutor(registry)
	e.SetConfirmFn(func(req common.ConfirmRequest) bool {
		return req.Level < common.LevelWrite
	})
	return &Engine{llmClient: llmClient, toolRegistry: registry, executor: e, mergeClient: mergeClient}
}


// Run executes a subagent and returns a structured Result.
// Pattern from Claude Code's runAgent() + finalizeAgentTool():
//   - Independent context with fresh file cache
//   - Structured output parsing after completion
//   - Safety classification before handoff
//   - Partial result recovery on error/interrupt
//   - Metadata tracking (tokens, tool calls, duration) for main agent assessment
func (e *Engine) Run(ctx context.Context, cfg RunConfig) (*Result, error) {
	subLog := debug.Sub(cfg.AgentType.Name)
	subLog("start: prompt=%q", cfg.Prompt[:min(len(cfg.Prompt), 120)])
	defer func(start time.Time) {
		subLog("done: duration=%v", time.Since(start).Round(time.Millisecond))
	}(time.Now())

	startTime := time.Now()
	var toolUseCount int
	var totalTokens int

	systemPrompt := cfg.AgentType.SystemPrompt
	if cfg.AgentType.Name == "researcher" && cfg.Thoroughness == thoroughDeep {
		systemPrompt = strings.Replace(systemPrompt,
			"Focus on the specific question. For \"very thorough\": search across multiple directories and naming conventions.",
			"Search across ALL packages, naming conventions, and locations. Read at least 5 files. Be exhaustive.", 1)
	}
	if cfg.Handoff != "" {
		systemPrompt += "\n\n<handoff>\n" + cfg.Handoff + "\n</handoff>"
	}

	// Seed subagent cache with main agent's cached files to avoid cold-start
	// disk reads. mtime/size checks in Lines() still guard against stale data.
	// The globalCacheMu serializes the save/swap/restore sequence so concurrent
	// subagents don't overwrite each other's saved cache references.
	globalCacheMu := tools.GlobalCacheMu()
	globalCacheMu.Lock()
	savedCache := tools.GetGlobalFileCache()
	subCache := tools.NewFileStateCache()
	subCache.Seed(savedCache)
	tools.SetGlobalFileCache(subCache)
	globalCacheMu.Unlock()
	defer func() {
		globalCacheMu.Lock()
		savedCache.Merge(subCache)
		tools.SetGlobalFileCache(savedCache)
		globalCacheMu.Unlock()
	}()

	ctxMgr := ctxmgr.NewSub(systemPrompt, cfg.ContextWindow, e.mergeClient)

	if cfg.Cwd != "" {
		ctxMgr.Add("system", ctxfmt.FormatCwd(cfg.Cwd))
	}
	if cfg.ProjectContext != "" && !cfg.AgentType.OmitProjectContext {
		ctxMgr.Add("system", cfg.ProjectContext)
	}
	if cfg.ConfirmFn != nil {
		prev := e.executor.ConfirmFn()
		e.executor.SetConfirmFn(cfg.ConfirmFn)
		defer e.executor.SetConfirmFn(prev)
	}
	if cfg.DisableThinking {
		prev := e.llmClient.GetDisableThinking()
		e.llmClient.SetDisableThinking(true)
		defer e.llmClient.SetDisableThinking(prev)
	}
	ctxMgr.Add("user", cfg.Prompt)

	phase := func(p string) {
		if cfg.OnPhase != nil {
			cfg.OnPhase(p)
		}
	}
	phase("Waiting")

	var readOnlyStreak int
	var lastText string // last assistant text content (for partial result recovery)

	// Wrap AddTokens to track total tokens for metadata reporting.
	localAddTokens := func(prompt, compl int) {
		totalTokens += prompt + compl
		if cfg.AddTokens != nil {
			cfg.AddTokens(prompt, compl)
		}
	}

	makeMeta := func() runMeta {
		hit, miss := ctxMgr.Tracker.CacheStats()
		return runMeta{totalTokens: totalTokens, toolUseCount: toolUseCount, durationMs: time.Since(startTime).Milliseconds(), cacheHitTokens: hit, cacheMissTokens: miss}
	}
	for step := 0; ; step++ {
		select {
		case <-ctx.Done():
			subLog("interrupted: step=%d lastText=%q", step, lastText[:min(len(lastText), 200)])
			return buildPartialResult(lastText, makeMeta()), ctx.Err()
		default:
		}

		if step >= maxSubAgentSteps {
			subLog("max steps reached: step=%d", step)
			return buildPartialResult(lastText, makeMeta()), nil
		}

		ctxMgr.AutoCompactIfNeeded()

		calls, text, err := e.reason(ctx, ctxMgr, cfg.AgentType.Tools, localAddTokens, phase)
		if err != nil {
			subLog("error: %v", err)
			if lastText != "" {
				subLog("partial_result: %q", lastText[:min(len(lastText), 300)])
				return buildPartialResult(lastText, makeMeta()), nil
			}
			return buildFailedResult(err.Error(), makeMeta()), err
		}

		if text != "" {
			lastText = text
		}

		if len(calls) == 0 {
			phase("done")
			result := buildResult(text, makeMeta())
			subLog("result: tokens=%d tools=%d duration=%dms output=%q",
				result.TotalTokens, result.ToolUseCount, result.DurationMs,
				text[:min(len(text), 300)])
			return result, nil
		}

		toolUseCount += len(calls)

		var toolNames []string
		for _, c := range calls {
			toolNames = append(toolNames, c.Name)
			phase("Running " + c.Name)
			// Notify parent via callback (tool_start).
			if cfg.OnToolCall != nil {
				cfg.OnToolCall(ToolCallEvent{
					Action: "tool_start", ToolName: c.Name,
					ToolArgs: tools.FormatArgs(c.Args),
				})
			}
		}
		subLog("tools: %v", toolNames)
		results := e.executor.ExecuteBatch(ctx, calls)
		batch := make([]ctxmgr.ToolResultMsg, len(results))
		for i, r := range results {
			content := r.EffectiveOutput()
			batch[i] = ctxmgr.ToolResultMsg{
				Message:  types.Message{Content: content, ToolCallID: r.ID},
				ToolName: calls[i].Name,
			}
			// Notify parent via callback (execute_tool).
			if cfg.OnToolCall != nil {
				cfg.OnToolCall(ToolCallEvent{
					Action: "execute_tool", ToolName: calls[i].Name,
					ToolArgs: tools.FormatArgs(calls[i].Args), Output: content,
				})
			}
		}
		ctxMgr.AddToolResultsBatch(batch)

		// Read-only spiral check — inject AFTER tool results so the
		// assistant→tool message chain stays contiguous.
		if subAllExploration(calls) {
			readOnlyStreak++
			if readOnlyStreak >= 3 {
				ctxMgr.Add("user", "[System] You've been reading without acting. Summarize your findings now — don't read any more files.")
				readOnlyStreak = 0
			}
		} else {
			readOnlyStreak = 0
		}

		phase("Waiting")
	}

}

// runMeta carries execution statistics through the result builders.
type runMeta struct {
	totalTokens     int
	toolUseCount    int
	durationMs      int64
	cacheHitTokens  int
	cacheMissTokens int
}

// buildResult constructs a Result from a completed subagent run.
func buildResult(rawOutput string, meta runMeta) *Result {
	r := &Result{
		Status: StatusCompleted, Content: rawOutput,
		TotalTokens: meta.totalTokens, ToolUseCount: meta.toolUseCount, DurationMs: meta.durationMs,
		CacheHitTokens: meta.cacheHitTokens, CacheMissTokens: meta.cacheMissTokens,
	}
	r.classification = classifyHandoff(rawOutput)
	return r
}

// buildPartialResult creates a Result for interrupted/killed subagents.
func buildPartialResult(lastText string, meta runMeta) *Result {
	r := &Result{
		Status: StatusPartial, Content: lastText,
		TotalTokens: meta.totalTokens, ToolUseCount: meta.toolUseCount, DurationMs: meta.durationMs,
		CacheHitTokens: meta.cacheHitTokens, CacheMissTokens: meta.cacheMissTokens,
	}
	r.classification = classUnavailable
	return r
}

// buildFailedResult creates a Result for subagents that produced no output.
func buildFailedResult(errMsg string, meta runMeta) *Result {
	return &Result{
		Status: StatusFailed, Content: errMsg,
		TotalTokens: meta.totalTokens, ToolUseCount: meta.toolUseCount, DurationMs: meta.durationMs,
		CacheHitTokens: meta.cacheHitTokens, CacheMissTokens: meta.cacheMissTokens,
		classification: classUnavailable,
	}
}

func (e *Engine) reason(ctx context.Context, mgr *ctxmgr.Manager, allowed []string, addTokens func(int, int), phase func(string)) ([]tools.ToolCallItem, string, error) {
	var calls []tools.ToolCallItem
	var textContent string
	var reasoningContent string

	toolDefs := e.filteredToolDefs(allowed)

	firstAttempt := true
	err := llm.Retry(ctx, llm.DefaultRetryConfig, func() error {
		result, err := tools.CallLLM(e.llmClient, tools.LLMCallOptions{
			Ctx:      ctx,
			Messages: mgr.Build(true),
			ToolDefs: toolDefs,
			Callbacks: tools.StreamCallbacks{
				OnPhase: phase,
				AddTokens: func(p, c int) {
					if addTokens != nil {
						addTokens(p, c)
					}
				},
			},
			CheckDone:      func() bool { return false },
			EstimatePrompt: firstAttempt,
		})
		if err != nil {
			return err
		}

		if firstAttempt {
			firstAttempt = false
		}

		textContent = result.Text
		reasoningContent = result.Reasoning
		calls = result.ToolCalls
		return nil
	})
	if err != nil {
		return nil, "", err
	}

	if len(calls) > 0 {
		mgr.AddAssistantToolCall(textContent, reasoningContent, tools.ToLLMToolCalls(calls))
	}
	return calls, textContent, nil
}

func (e *Engine) filteredToolDefs(allowed []string) []types.ToolDef {
	all := e.toolRegistry.Descriptors()
	set := make(map[string]bool, len(allowed))
	for _, n := range allowed {
		set[n] = true
	}
	var filtered []tools.Descriptor
	for _, d := range all {
		if d.Name == taskToolName {
			continue // sub-agents cannot spawn sub-agents
		}
		if set[d.Name] {
			filtered = append(filtered, d)
		}
	}
	return tools.ToToolDefs(filtered)
}

// subAllExploration returns true if every call is read-only exploration.
func subAllExploration(calls []tools.ToolCallItem) bool {
	if len(calls) == 0 {
		return false
	}
	for _, c := range calls {
		switch c.Name {
		case "read", "grep", "glob", "list", "web_search", "web_fetch":
			continue
		default:
			return false
		}
	}
	return true
}

