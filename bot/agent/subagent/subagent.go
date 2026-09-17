package subagent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"nekocode/bot/agent/internal/kernel"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/agentprofile"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/runner"
	"nekocode/bot/policy"
	"nekocode/bot/policy/builtin"
	"nekocode/bot/provider"
	providertypes "nekocode/bot/provider/types"
	"nekocode/logger"
)

const (
	taskToolName                 = "task"
	submitResultToolName         = "nekocode_submit_result"
	maxSubAgentSteps             = agentprofile.MaxSteps
	maxCompletionProtocolRetries = 2
)

type Engine struct {
	llmClient       provider.LLM
	toolRegistry    *tools.Registry
	compactionModel provider.LLM
}

// Config contains the services shared by subagent runs.
type Config struct {
	LLM             provider.LLM
	Tools           *tools.Registry
	CompactionModel provider.LLM
}

type runState struct {
	startTime         time.Time
	toolUseCount      int
	totalTokens       int
	completionRetries int
	lastText          string
}

// New creates a reusable subagent engine.
func New(config Config) *Engine {
	return &Engine{
		llmClient: config.LLM, toolRegistry: config.Tools, compactionModel: config.CompactionModel,
	}
}

func newRunState() *runState {
	return &runState{startTime: time.Now()}
}

func newRunGuard() *policy.Policy {
	guard := policy.New()
	builtin.Register(guard)
	return guard
}

func (s *runState) addTokens(cfg RunConfig) func(int, int) {
	return func(prompt, compl int) {
		s.totalTokens += prompt + compl
		if cfg.AddTokens != nil {
			cfg.AddTokens(prompt, compl)
		}
	}
}

func (s *runState) meta(ctxMgr *ctxmgr.Manager) runMeta {
	status := ctxMgr.Status()
	return runMeta{
		totalTokens:     s.totalTokens,
		toolUseCount:    s.toolUseCount,
		durationMs:      time.Since(s.startTime).Milliseconds(),
		cacheHitTokens:  status.CacheHit,
		cacheMissTokens: status.CacheMiss,
	}
}

func (s *runState) recordCalls(calls []core.ToolCallItem) {
	s.toolUseCount += len(calls)
}

func (e *Engine) Run(ctx context.Context, cfg RunConfig) (*Result, error) {
	if cfg.Profile.MaxSteps < 0 || cfg.Profile.MaxSteps > maxSubAgentSteps {
		return nil, fmt.Errorf("max_steps must be between 0 and %d", maxSubAgentSteps)
	}
	cfg.Profile = agentprofile.Clone(cfg.Profile)
	if e.toolRegistry.Has(submitResultToolName) {
		return nil, fmt.Errorf("sub-agent tool registry contains reserved lifecycle tool name %q", submitResultToolName)
	}
	cfg.guard = newRunGuard()
	profile := cfg.Profile
	subLog := logger.Sub(profile.Name)
	subLog("start: prompt=%q", cfg.Prompt[:min(len(cfg.Prompt), 120)])
	defer func(start time.Time) {
		subLog("done: duration=%v", time.Since(start).Round(time.Millisecond))
	}(time.Now())

	state := newRunState()
	executor, cleanupExecutor := e.newExecutor(cfg)
	defer cleanupExecutor()

	ctxMgr := e.newContextManager(cfg)
	ctxMgr.SetSessionIDProvider(func() string { return cfg.SessionID })
	ctxMgr.SetLLMUsageRecorder(func(usage providertypes.StreamUsage) {
		state.addTokens(cfg)(usage.PromptTokens, usage.CompletionTokens)
		if cfg.RecordLLMUsage != nil {
			cfg.RecordLLMUsage(usage)
		}
	})

	ctxMgr.Add("user", buildTaskPrompt(cfg))
	phase := phaseReporter(cfg)
	phase("Waiting")

	run := &engineRun{
		engine:   e,
		ctx:      ctx,
		cfg:      cfg,
		ctxMgr:   ctxMgr,
		executor: executor,
		state:    state,
		phase:    phase,
		log:      subLog,
	}

	kernel.RunLoop(kernel.Loop{
		StepLimitReached: run.stepLimitReached,
		Step:             run.stepOnce,
	})

	return run.finish()
}

type engineRun struct {
	engine   *Engine
	ctx      context.Context
	cfg      RunConfig
	ctxMgr   *ctxmgr.Manager
	executor *runner.Executor
	state    *runState
	phase    func(string)
	log      func(string, ...any)

	step   int
	result *Result
	err    error
}

func (r *engineRun) stepLimitReached() bool {
	limit := r.cfg.Profile.MaxSteps
	if limit == 0 {
		limit = maxSubAgentSteps
	}
	if r.step < limit {
		return false
	}
	r.log("max steps reached: step=%d", r.step)
	r.result = buildProtocolPartialResult(r.state.lastText, "sub-agent step limit reached before submit_result", r.state.meta(r.ctxMgr))
	return true
}

func (r *engineRun) stepOnce() bool {
	if r.ctx.Err() != nil {
		r.log("interrupted: step=%d lastText=%q", r.step, r.state.lastText[:min(len(r.state.lastText), 200)])
		r.result = buildPartialResult(r.state.lastText, r.state.meta(r.ctxMgr))
		r.err = r.ctx.Err()
		return true
	}

	if _, err := r.ctxMgr.AutoCompactIfNeeded(r.ctx); err != nil {
		r.log("compact error: %v", err)
		if r.state.lastText != "" {
			r.result = buildPartialResult(r.state.lastText, r.state.meta(r.ctxMgr))
		} else {
			r.result = buildFailedResult(err.Error(), r.state.meta(r.ctxMgr))
		}
		r.err = err
		return true
	}
	profile := r.cfg.Profile
	calls, text, err := r.engine.reason(r.ctx, r.ctxMgr, profile.Tools, buildSkillWorkflow(r.cfg), r.state.addTokens(r.cfg), r.cfg.RecordLLMUsage, r.cfg.SessionID, r.phase, r.cfg)
	r.ctxMgr.SetHints("")
	if err != nil {
		r.log("error: %v", err)
		if r.state.lastText != "" {
			r.log("partial_result: %q", r.state.lastText[:min(len(r.state.lastText), 300)])
			r.result = buildPartialResult(r.state.lastText, r.state.meta(r.ctxMgr))
			return true
		}
		r.result = buildFailedResult(err.Error(), r.state.meta(r.ctxMgr))
		r.err = err
		return true
	}

	if text != "" {
		r.state.lastText = text
	}
	if handoff, submitted, protocolErr := submittedResult(calls); submitted {
		if protocolErr == nil {
			r.complete(handoff)
			return true
		}
		r.rejectToolBatch(calls, protocolErr.Error())
		return r.retryCompletionProtocol(protocolErr.Error())
	}
	if len(calls) == 0 {
		return r.retryCompletionProtocol("response ended without submit_result")
	}

	r.state.completionRetries = 0
	r.state.recordCalls(calls)
	r.engine.executeToolBatch(r.ctx, r.cfg, r.ctxMgr, r.executor, calls, r.state, r.phase, r.log)
	r.phase("Waiting")
	r.step++
	return false
}

func submittedResult(calls []core.ToolCallItem) (handoff Handoff, submitted bool, err error) {
	for _, call := range calls {
		if call.Name != submitResultToolName {
			continue
		}
		if len(calls) != 1 {
			return Handoff{}, true, fmt.Errorf("%s must be the only tool call in its batch", submitResultToolName)
		}
		var parseErr error
		handoff.Summary, parseErr = requiredResultString(call.Args, "summary")
		if parseErr == nil {
			handoff.Evidence, parseErr = requiredResultStrings(call.Args, "evidence")
		}
		if parseErr == nil {
			handoff.Files, parseErr = requiredResultStrings(call.Args, "files")
		}
		if parseErr == nil {
			handoff.Verification, parseErr = requiredResultString(call.Args, "verification")
		}
		if parseErr == nil {
			handoff.Unfinished, parseErr = requiredResultStrings(call.Args, "unfinished")
		}
		if parseErr == nil {
			handoff.Risks, parseErr = requiredResultStrings(call.Args, "risks")
		}
		if parseErr != nil {
			return Handoff{}, true, fmt.Errorf("%s: %w", submitResultToolName, parseErr)
		}
		return handoff, true, nil
	}
	return Handoff{}, false, nil
}

func requiredResultString(args map[string]any, name string) (string, error) {
	value, ok := args[name].(string)
	value = strings.TrimSpace(value)
	if !ok || value == "" {
		return "", fmt.Errorf("requires a non-empty %s field", name)
	}
	return value, nil
}

func requiredResultStrings(args map[string]any, name string) ([]string, error) {
	raw, ok := args[name].([]any)
	if !ok {
		return nil, fmt.Errorf("requires field %s to be an array", name)
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		value, ok := item.(string)
		value = strings.TrimSpace(value)
		if !ok || value == "" {
			return nil, fmt.Errorf("requires %s to contain only non-empty strings", name)
		}
		values = append(values, value)
	}
	return values, nil
}

func (r *engineRun) rejectToolBatch(calls []core.ToolCallItem, reason string) {
	results := make([]ctxmgr.ToolResultMsg, len(calls))
	for i, call := range calls {
		results[i] = ctxmgr.ToolResultMsg{
			Message: providertypes.Message{
				Content: reason, ToolCallID: call.ID, IsError: true,
			},
			ToolName: call.Name,
		}
	}
	r.ctxMgr.AddToolResultsBatch(results)
}

func (r *engineRun) retryCompletionProtocol(reason string) bool {
	if r.state.completionRetries >= maxCompletionProtocolRetries {
		r.result = buildProtocolPartialResult(r.state.lastText, reason, r.state.meta(r.ctxMgr))
		r.phase("done")
		return true
	}
	r.state.completionRetries++
	r.ctxMgr.Add("user",
		"[Sub-agent completion protocol]\nYour response was not submitted. Continue the delegated task, then call "+submitResultToolName+" as the only tool call with every required handoff field. Plain assistant text does not complete the task.",
		providertypes.MessageSourceRuntimeEvent,
	)
	r.phase("Waiting")
	r.step++
	return false
}

func (r *engineRun) complete(handoff Handoff) {
	r.phase("done")
	r.result = buildHandoffResult(handoff, r.state.meta(r.ctxMgr))
	r.log("result: tokens=%d tools=%d duration=%dms output=%q",
		r.result.TotalTokens, r.result.ToolUseCount, r.result.DurationMs,
		r.result.Content[:min(len(r.result.Content), 300)])
}

func (r *engineRun) finish() (*Result, error) {
	if r.result == nil {
		r.result = buildPartialResult(r.state.lastText, r.state.meta(r.ctxMgr))
	}
	return r.result, r.err
}

type Status int

const (
	StatusCompleted Status = iota
	StatusFailed
	StatusPartial
)

type Result struct {
	Status          Status
	Content         string
	TotalTokens     int
	ToolUseCount    int
	DurationMs      int64
	CacheHitTokens  int
	CacheMissTokens int
	Handoff         *Handoff
}

type Handoff struct {
	Summary      string
	Evidence     []string
	Files        []string
	Verification string
	Unfinished   []string
	Risks        []string
}

type runMeta struct {
	totalTokens     int
	toolUseCount    int
	durationMs      int64
	cacheHitTokens  int
	cacheMissTokens int
}

func FormatResult(r *Result) string {
	return r.Content
}

func newResult(status Status, content string, meta runMeta) *Result {
	return &Result{
		Status:          status,
		Content:         content,
		TotalTokens:     meta.totalTokens,
		ToolUseCount:    meta.toolUseCount,
		DurationMs:      meta.durationMs,
		CacheHitTokens:  meta.cacheHitTokens,
		CacheMissTokens: meta.cacheMissTokens,
	}
}

func buildResult(rawOutput string, meta runMeta) *Result {
	return newResult(StatusCompleted, rawOutput, meta)
}

func buildHandoffResult(handoff Handoff, meta runMeta) *Result {
	result := newResult(StatusCompleted, formatHandoff(handoff), meta)
	copy := handoff
	copy.Evidence = append([]string(nil), handoff.Evidence...)
	copy.Files = append([]string(nil), handoff.Files...)
	copy.Unfinished = append([]string(nil), handoff.Unfinished...)
	copy.Risks = append([]string(nil), handoff.Risks...)
	result.Handoff = &copy
	return result
}

func formatHandoff(handoff Handoff) string {
	var out strings.Builder
	out.WriteString(handoff.Summary)
	writeHandoffList(&out, "Evidence", handoff.Evidence)
	writeHandoffList(&out, "Files", handoff.Files)
	out.WriteString("\n\nVerification:\n")
	out.WriteString(handoff.Verification)
	writeHandoffList(&out, "Unfinished", handoff.Unfinished)
	writeHandoffList(&out, "Risks", handoff.Risks)
	return out.String()
}

func writeHandoffList(out *strings.Builder, heading string, items []string) {
	out.WriteString("\n\n")
	out.WriteString(heading)
	out.WriteString(":")
	if len(items) == 0 {
		out.WriteString("\n- None")
		return
	}
	for _, item := range items {
		out.WriteString("\n- ")
		out.WriteString(item)
	}
}

func buildPartialResult(lastText string, meta runMeta) *Result {
	return newResult(StatusPartial, lastText, meta)
}

func buildFailedResult(errMsg string, meta runMeta) *Result {
	return newResult(StatusFailed, errMsg, meta)
}

func buildProtocolPartialResult(lastText, reason string, meta runMeta) *Result {
	content := "Sub-agent did not submit a structured result: " + reason
	if text := strings.TrimSpace(lastText); text != "" {
		content += "\n\nLast unsubmitted response:\n" + text
	}
	return buildPartialResult(content, meta)
}
