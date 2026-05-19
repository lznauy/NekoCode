package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"nekocode/bot/ctxmgr"
	ctxfmt "nekocode/bot/ctxmgr/context"
	"nekocode/bot/ctxmgr/compact"
	"nekocode/bot/tools"
	"nekocode/llm"

	"nekocode/common"
)

const (
	thoroughQuick  = "quick"
	thoroughDeep   = "very thorough"
)

// Engine runs a sub-agent loop. Fully self-contained — does not import agent.
type Engine struct {
	llmClient    llm.LLM
	toolRegistry *tools.Registry
	executor     *tools.Executor
}

func NewEngine(llmClient llm.LLM, registry *tools.Registry) *Engine {
	e := tools.NewExecutor(registry)
	// Auto-approve write-level tools for subagents — the main agent already
	// obtained user approval for the delegated task. Destructive tools (LevelDestructive,
	// LevelForbidden) are still blocked by the executor's level check.
	e.SetConfirmFn(func(req common.ConfirmRequest) bool {
		return req.Level <= common.LevelWrite
	})
	return &Engine{llmClient: llmClient, toolRegistry: registry, executor: e}
}


// toolCallItem mirrors agent.ToolCallItem to avoid circular imports.
type toolCallItem struct {
	ID   string
	Name string
	Args map[string]interface{}
}

// Run executes a subagent and returns a structured Result.
// Pattern from Claude Code's runAgent() + finalizeAgentTool():
//   - Independent context with fresh file cache
//   - Structured output parsing after completion
//   - Safety classification before handoff
//   - Partial result recovery on error/interrupt
//   - Metadata tracking (tokens, tool calls, duration) for main agent assessment
func (e *Engine) Run(ctx context.Context, cfg RunConfig) (*Result, error) {
	startTime := time.Now()
	var toolUseCount int
	var totalTokens int

	// Apply thoroughness-based overrides for the explore agent.
	tokenBudget := cfg.TokenBudget / 10
	if tokenBudget < 8000 {
		tokenBudget = 8000
	}
	systemPrompt := cfg.AgentType.SystemPrompt
	if cfg.AgentType.Name == "explore" {
		switch cfg.Thoroughness {
		case thoroughDeep:
			systemPrompt = strings.Replace(systemPrompt,
				"Focus on the specific question. For \"very thorough\": search across multiple directories and naming conventions.",
				"Search across ALL packages, naming conventions, and locations. Read at least 5 files. Be exhaustive.", 1)
		}
	}

	// Sub-agents have isolated contexts — they can't reference file content
	// from the main agent's conversation. Swap in a fresh cache so their
	// first read of any file returns full content, not a stub.
	// Merge subagent cache back on completion so the main agent benefits
	// from files the subagent discovered.
	savedCache := tools.GlobalFileCache
	subCache := tools.NewFileStateCache()
	tools.GlobalFileCache = subCache
	defer func() {
		savedCache.Merge(subCache)
		tools.GlobalFileCache = savedCache
	}()

	ctxMgr := ctxmgr.New(systemPrompt, nil)
	ctxMgr.SetTokenBudget(tokenBudget)
	ctxMgr.SetSummarizer(e.makeSummarizer(ctx))

	if cfg.Cwd != "" {
		ctxMgr.Add("system", ctxfmt.FormatCwd(cfg.Cwd))
	}
	if cfg.ProjectContext != "" && !cfg.AgentType.OmitProjectContext {
		ctxMgr.Add("system", cfg.ProjectContext)
	}
	if cfg.DisableThinking {
		e.llmClient.SetDisableThinking(true)
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

	for step := 0; ; step++ {
		select {
		case <-ctx.Done():
			meta := runMeta{totalTokens: totalTokens, toolUseCount: toolUseCount, durationMs: time.Since(startTime).Milliseconds()}
			return buildPartialResult(lastText, meta, cfg.AgentType.Name), ctx.Err()
		default:
		}

		
		calls, text, err := e.reason(ctx, ctxMgr, cfg.AgentType.Tools, localAddTokens, phase)
		if err != nil {
			if lastText != "" {
				meta := runMeta{totalTokens: totalTokens, toolUseCount: toolUseCount, durationMs: time.Since(startTime).Milliseconds()}
				return buildPartialResult(lastText, meta, cfg.AgentType.Name), nil
			}
			meta := runMeta{totalTokens: totalTokens, toolUseCount: toolUseCount, durationMs: time.Since(startTime).Milliseconds()}
			return buildFailedResult(err.Error(), meta), err
		}

		if text != "" {
			lastText = text
		}

		if len(calls) == 0 {
			phase("done")
			meta := runMeta{totalTokens: totalTokens, toolUseCount: toolUseCount, durationMs: time.Since(startTime).Milliseconds()}
			return buildResult(text, meta, cfg.AgentType.Name), nil
		}

		toolUseCount += len(calls)

		for _, c := range calls {
			phase("Running " + c.Name)
		}
		items := make([]tools.ToolCallItem, len(calls))
		for i, c := range calls {
			items[i] = tools.ToolCallItem{ID: c.ID, Name: c.Name, Args: c.Args}
		}
		results := e.executor.ExecuteBatch(ctx, items)
		batch := make([]ctxmgr.ToolResultMsg, len(results))
		for i, r := range results {
			content := r.Output
			if r.Error != "" {
				content = r.Error
			}
			batch[i] = ctxmgr.ToolResultMsg{
				Message:  llm.Message{Content: content, ToolCallID: r.ID},
				ToolName: calls[i].Name,
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
	totalTokens  int
	toolUseCount int
	durationMs   int64
}

// buildResult constructs a Result from a completed subagent run.
func buildResult(rawOutput string, meta runMeta, agentType string) *Result {
	content, keyFiles, filesChanged, issues := parseStructuredOutput(rawOutput)

	if content == "" {
		content = rawOutput
	}

	r := &Result{
		Status:       StatusCompleted,
		Content:      content,
		KeyFiles:     keyFiles,
		FilesChanged: filesChanged,
		Issues:       issues,
		TotalTokens:  meta.totalTokens,
		ToolUseCount: meta.toolUseCount,
		DurationMs:   meta.durationMs,
	}

	r.classification = classifyHandoff(rawOutput, filesChanged, keyFiles)

	return r
}

// buildPartialResult creates a Result for interrupted/killed subagents.
func buildPartialResult(lastText string, meta runMeta, agentType string) *Result {
	content, keyFiles, filesChanged, issues := parseStructuredOutput(lastText)
	if content == "" {
		content = lastText
	}
	r := &Result{
		Status:       StatusPartial,
		Content:      content,
		KeyFiles:     keyFiles,
		FilesChanged: filesChanged,
		Issues:       append(issues, "subagent was interrupted before completion"),
		TotalTokens:  meta.totalTokens,
		ToolUseCount: meta.toolUseCount,
		DurationMs:   meta.durationMs,
	}

	r.classification = classifyHandoff(lastText, filesChanged, keyFiles)

	return r
}

// buildFailedResult creates a Result for subagents that produced no output.
func buildFailedResult(errMsg string, meta runMeta) *Result {
	return &Result{
		Status:         StatusFailed,
		Content:        errMsg,
		Issues:         []string{errMsg},
		TotalTokens:    meta.totalTokens,
		ToolUseCount:   meta.toolUseCount,
		DurationMs:     meta.durationMs,
		classification: classUnavailable,
	}
}

func (e *Engine) makeSummarizer(ctx context.Context) compact.Summarizer {
	return func(msgs []llm.Message, prevSummary string) (string, error) {
		prompt := compact.BuildPrompt(msgs, prevSummary)
		resp, err := e.llmClient.Chat(ctx, []llm.Message{{Role: "user", Content: prompt}}, nil)
		if err != nil {
			return "", err
		}
		if len(resp.Choices) > 0 {
			return resp.Choices[0].Message.Content, nil
		}
		return "", nil
	}
}

func (e *Engine) reason(ctx context.Context, mgr *ctxmgr.Manager, allowed []string, addTokens func(int, int), phase func(string)) ([]toolCallItem, string, error) {
	var calls []toolCallItem
	var textContent string
	var reasoningContent string

	firstAttempt := true
	err := llm.Retry(ctx, llm.DefaultRetryConfig, func() error {
		messages := mgr.Build(true)
		toolDefs := e.filteredToolDefs(allowed)

		tokenCh, errCh := e.llmClient.ChatStream(ctx, messages, toolDefs)
		if tokenCh == nil {
			select {
			case err := <-errCh:
				return err
			default:
				return fmt.Errorf("chat stream failed")
			}
		}

		var textBuf strings.Builder
		var reasoningBuf strings.Builder
		tcAccum := make(map[int]*toolAccum)

		estPrompt := 0
		promptChars := 0
		for _, m := range messages {
			promptChars += len(m.Content) + len(m.Role)
		}
		estPrompt = promptChars / 4
		if firstAttempt && addTokens != nil {
			addTokens(estPrompt, 0)
			firstAttempt = false
		}
		estCompl := 0
		var lastUsage *llm.StreamUsage

		firstReasoning := true
		phaseThink := true
		for token := range tokenCh {
			if firstReasoning && token.ReasoningContent != "" {
				firstReasoning = false
				if phase != nil {
					phase(common.PhaseThinking)
				}
			}
			if token.Content != "" {
				if phaseThink {
					phaseThink = false
					if phase != nil {
						phase(common.PhaseReasoning)
					}
				}
				textBuf.WriteString(token.Content)
				if addTokens != nil {
					addTokens(0, 1)
				}
				estCompl++
			}
			if token.ReasoningContent != "" {
				reasoningBuf.WriteString(token.ReasoningContent)
			}
			if token.Usage != nil {
				lastUsage = token.Usage
			}
			if token.ToolCallDelta != nil {
				if phaseThink {
					phaseThink = false
					if phase != nil {
						phase(common.PhaseReasoning)
					}
				}
				idx := token.ToolCallDelta.Index
				acc := tcAccum[idx]
				if acc == nil {
					acc = &toolAccum{}
					tcAccum[idx] = acc
				}
				if token.ToolCallDelta.ID != "" {
					acc.id = token.ToolCallDelta.ID
				}
				if token.ToolCallDelta.Name != "" {
					acc.name = token.ToolCallDelta.Name
				}
				acc.args.WriteString(token.ToolCallDelta.Arguments)
				if addTokens != nil {
					addTokens(0, 1)
				}
				estCompl++
			}
		}

		// Record actual API usage to calibrate heuristic estimates.
		if lastUsage != nil {
			if lastUsage.PromptTokens > 0 || lastUsage.CompletionTokens > 0 {
				if addTokens != nil {
					addTokens(lastUsage.PromptTokens-estPrompt, lastUsage.CompletionTokens-estCompl)
				}
				mgr.RecordUsage(lastUsage.PromptTokens, lastUsage.CompletionTokens)
			}
			if lastUsage.CacheHitTokens > 0 || lastUsage.CacheMissTokens > 0 {
				mgr.RecordCache(lastUsage.CacheHitTokens, lastUsage.CacheMissTokens)
			}
		}

		select {
		case err := <-errCh:
			if err != nil {
				return err
			}
		default:
		}

		textContent = tools.StripAnsi(textBuf.String())
		reasoningContent = reasoningBuf.String()

		if len(tcAccum) == 0 {
			return nil
		}

		calls = make([]toolCallItem, 0, len(tcAccum))
		for i := 0; i < len(tcAccum); i++ {
			acc := tcAccum[i]
			if acc == nil {
				continue
			}
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(acc.args.String()), &args); err != nil {
				return fmt.Errorf("failed to parse tool arguments: %v", err)
			}
			calls = append(calls, toolCallItem{ID: acc.id, Name: acc.name, Args: args})
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}

	if len(calls) > 0 {
		mgr.AddAssistantToolCall(textContent, reasoningContent, toLLMToolCalls(calls))
	}
	return calls, textContent, nil
}

func (e *Engine) filteredToolDefs(allowed []string) []llm.ToolDef {
	all := e.toolRegistry.Descriptors()
	set := make(map[string]bool, len(allowed))
	for _, n := range allowed {
		set[n] = true
	}
	var filtered []tools.Descriptor
	for _, d := range all {
		if set[d.Name] {
			filtered = append(filtered, d)
		}
	}
	return tools.ToToolDefs(filtered)
}

// --- helpers ---

type toolAccum struct {
	id   string
	name string
	args strings.Builder
}

func toLLMToolCalls(calls []toolCallItem) []llm.ToolCall {
	out := make([]llm.ToolCall, len(calls))
	for i, c := range calls {
		args, _ := json.Marshal(c.Args)
		out[i] = llm.ToolCall{
			ID:       c.ID,
			Type:     "function",
			Function: llm.FunctionCall{Name: c.Name, Arguments: string(args)},
		}
	}
	return out
}

// subAllExploration returns true if every call is read-only exploration.
func subAllExploration(calls []toolCallItem) bool {
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
