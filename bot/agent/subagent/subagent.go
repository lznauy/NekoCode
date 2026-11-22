package subagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"nekocode/bot/agent/internal/kernel"
	"nekocode/bot/agent/internal/toolpolicy"
	"nekocode/bot/calllog"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/agentprofile"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/bot/extension/tool/runtime/runner"
	"nekocode/bot/policy"
	"nekocode/bot/policy/builtin"
	"nekocode/bot/provider"
	providertypes "nekocode/bot/provider/types"
	"nekocode/logger"
	"nekocode/protocol"
	"nekocode/util/modelbudget"
)

const (
	taskToolName                 = "task"
	submitResultToolName         = "nekocode_submit_result"
	maxSubAgentSteps             = agentprofile.DefaultMaxSteps
	maxCompletionProtocolRetries = 2
	// MaxEvidenceIDs bounds optional parent handoff association work.
	MaxEvidenceIDs = 16
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
	processWait       processWaitHistory
	writeDrafts       toolpolicy.WriteDrafts
	startTime         time.Time
	toolUseCount      int
	totalTokens       int
	completionRetries int
	lastText          string
	initialCacheHit   int
	initialCacheMiss  int
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

func (s *runState) meta(status ctxmgr.Status) runMeta {
	return runMeta{
		totalTokens:     s.totalTokens,
		toolUseCount:    s.toolUseCount,
		durationMs:      time.Since(s.startTime).Milliseconds(),
		cacheHitTokens:  status.CacheHit - s.initialCacheHit,
		cacheMissTokens: status.CacheMiss - s.initialCacheMiss,
	}
}

func (s *runState) recordCalls(calls []core.ToolCallItem) {
	s.toolUseCount += len(calls)
}

func (e *Engine) Run(ctx context.Context, cfg RunConfig) (result *Result, runErr error) {
	identity := protocol.ExecutionFromContext(ctx)
	if identity.RunID == "" {
		identity.RunID = uuid.NewString()
	}
	identity.TaskID, identity.AttemptID = cfg.SubagentID, cfg.AttemptID
	ctx = calllog.WithSession(protocol.WithExecutionIdentity(ctx, identity), cfg.SessionID)
	ctx = modelbudget.WithRole(ctx, modelbudget.Work, identity.TaskID+":"+identity.AttemptID)
	if cfg.Profile.MaxSteps < 0 || cfg.Profile.MaxSteps > agentprofile.MaxSteps {
		return nil, fmt.Errorf("max_steps must be between 0 and %d", agentprofile.MaxSteps)
	}
	if len(cfg.SkillNames) != 0 && len(cfg.SkillNames) != len(cfg.SkillContents) {
		return nil, fmt.Errorf("skill names and contents must have matching lengths")
	}
	cfg.SkillNames = append([]string(nil), cfg.SkillNames...)
	cfg.SkillContents = append([]string(nil), cfg.SkillContents...)
	cfg.Profile = agentprofile.Clone(cfg.Profile)
	ctx = protocol.WithProfileConfiguration(ctx, cfg.Profile.ConfigurationHash())
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
	ctxMgr.SetOutputStore(executor.ExecutionState().OutputStore)
	ctxMgr.SetSessionIDProvider(func() string { return cfg.SessionID })
	ctxMgr.SetLLMUsageRecorder(func(usage providertypes.StreamUsage) {
		state.addTokens(cfg)(usage.PromptTokens, usage.CompletionTokens)
		if cfg.RecordLLMUsage != nil {
			cfg.RecordLLMUsage(usage)
		}
	})

	if cfg.ResumeContext != nil {
		// Refresh trusted instructions rather than restoring old authority.
		restored := *cfg.ResumeContext
		restored.SystemPrompt = buildSystemPrompt(cfg)
		ctxMgr.Restore(restored)
		closeInterruptedTools(ctxMgr, restored.Messages)
	}
	// Mask any inherited reader, including standalone children with no durable identity.
	ctx = refbridge.WithReader(ctx, referenceReader(cfg, ctxMgr, executor.ExecutionState().OutputStore))
	// The restored tracker is cumulative; the parent records only this run's usage.
	initialUsage := ctxMgr.Status()
	state.initialCacheHit, state.initialCacheMiss = initialUsage.CacheHit, initialUsage.CacheMiss
	ctxMgr.Add("user", buildTaskPrompt(cfg))
	workflowsRecorded := false
	cfg.onRequestPrepared = func() {
		if workflowsRecorded {
			return
		}
		ctxMgr.RecordSkillWorkflows(cfg.SkillNames, cfg.SkillContents)
		workflowsRecorded = true
	}

	checkpoint := func(status, output string) error {
		if cfg.SaveContext == nil {
			return nil
		}
		snap := ctxMgr.Snapshot()
		contextStatus := ctxMgr.Status()
		meta := state.meta(contextStatus)
		usage := AttemptUsage{TotalTokens: meta.totalTokens, CacheHitTokens: meta.cacheHitTokens, CacheMissTokens: meta.cacheMissTokens, ToolUseCount: meta.toolUseCount, DurationMs: meta.durationMs, ContextTokens: contextStatus.Tokens, ContextWindow: contextStatus.Budget}
		if result != nil {
			usage.Summary = result.Summary
		}
		if err := cfg.SaveContext(snap, status, output, usage); err != nil {
			return protocol.MarkEvidenceIncomplete(err)
		}
		ctxMgr.AcknowledgeTranscript(snap.TranscriptBase, snap.TranscriptBase+len(snap.Transcript))
		return nil
	}
	if err := checkpoint("running", ""); err != nil {
		return nil, fmt.Errorf("save subagent: %w", err)
	}
	if cfg.OnStarted != nil {
		cfg.OnStarted()
	}
	defer func() {
		status, output := "failed", ""
		if result != nil {
			output = FormatResult(result)
			switch result.Status {
			case StatusCompleted:
				status = "completed"
			case StatusPartial:
				status = "partial"
			}
		}
		if runErr != nil {
			status = "failed"
		}
		if ctx.Err() != nil {
			if runErr == nil {
				runErr = ctx.Err()
			}
			status = "interrupted"
			if result != nil {
				result.Summary.StopReason = "cancelled"
				if result.Status == StatusCompleted {
					result.Status = StatusPartial
				}
			}
		}
		if err := checkpoint(status, output); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("save subagent: %w", err))
			if result != nil {
				result.Status = StatusFailed
				result.Summary.StopReason = "execution_error"
				if ctx.Err() != nil {
					result.Status = StatusPartial
					result.Summary.StopReason = "cancelled"
				}
			}
		}
		// A successful terminal checkpoint commits the sampled outcome.
		// Cancellation arriving during that write belongs after the commit.
	}()
	phase := phaseReporter(cfg)
	phase("Waiting")

	run := &engineRun{
		engine:     e,
		ctx:        ctx,
		cfg:        cfg,
		ctxMgr:     ctxMgr,
		executor:   executor,
		state:      state,
		phase:      phase,
		checkpoint: checkpoint,
		log:        subLog,
	}

	if cfg.ForegroundInterval > 0 {
		var cancel context.CancelFunc
		run.ctx, cancel = context.WithTimeoutCause(ctx, cfg.ForegroundInterval, errForegroundReview)
		defer cancel()
	}
	kernel.RunLoop(kernel.Loop{
		StepLimitReached: run.stepLimitReached,
		Step: func() bool {
			done := run.stepOnce()
			if done {
				return true
			}
			if err := checkpoint("running", ""); err != nil {
				run.err = fmt.Errorf("save subagent: %w", err)
				return true
			}
			return done
		},
	})

	// The parent context owns cancellation and the terminal checkpoint. An
	// expired scheduling interval only ends this attempt, after its executor
	// has unwound. Never detach a worker that can still edit the workspace.
	if ctx.Err() == nil && errors.Is(context.Cause(run.ctx), errForegroundReview) &&
		(run.result == nil || run.result.Handoff == nil) &&
		(run.err == nil || errors.Is(run.err, context.DeadlineExceeded) || errors.Is(run.err, context.Canceled)) && !protocol.EvidenceIncomplete(run.err) {
		return run.foregroundPartial(), nil
	}
	return run.finish()
}

type handoffPhase uint8

const (
	handoffWorking handoffPhase = iota
	handoffCorrecting
	handoffFinalizing
)

type engineRun struct {
	engine     *Engine
	ctx        context.Context
	cfg        RunConfig
	ctxMgr     *ctxmgr.Manager
	executor   *runner.Executor
	state      *runState
	phase      func(string)
	checkpoint func(string, string) error
	log        func(string, ...any)

	step               int
	handoffPhase       handoffPhase
	finalizationReason string
	result             *Result
	err                error
}

func (r *engineRun) stepLimitReached() bool {
	limit := r.cfg.Profile.MaxSteps
	if limit == 0 {
		limit = maxSubAgentSteps
	}
	reason := "step_limit"
	budget := modelbudget.FromContext(r.ctx)
	if budget != nil && budget.Snapshot().WorkRemaining == 0 {
		reason = "shared_budget"
	} else if r.cfg.ForegroundInterval > 0 && r.state.processWait.reviewTask != "" {
		reason = "process_wait_review"
	} else if r.step < limit {
		return false
	}
	if r.handoffPhase != handoffFinalizing {
		r.handoffPhase = handoffFinalizing
		r.finalizationReason = reason
		detail := ""
		if reason == "process_wait_review" {
			detail = r.state.processWait.description() + " Report the prerequisite, current owner if known, and what the coordinator can do next. "
		}
		r.ctxMgr.Add("user", "[Sub-agent stage handoff] "+detail+"Reason: "+reason+". Ordinary investigation is paused at a tool-batch boundary. Do not call ordinary tools. Call "+submitResultToolName+" as the only tool call. Preserve findings and decisive evidence. Put only actionable work remaining inside the delegated scope in unfinished; put unverified assumptions, blocked checks, and out-of-scope validation in verification_gaps. Include a concrete next action and stop condition for unfinished work. Do not mark remaining scoped work complete just because this stage ended. The coordinator can resume this same task and workspace after reviewing the evidence.", providertypes.MessageSourceRuntimeEvent)
		r.phase("Waiting")
		return false
	}

	r.log("stage handoff ended: reason=%s step=%d", r.finalizationReason, r.step)
	r.result = buildProtocolPartialResult(r.state.lastText, r.finalizationReason+" reached before submit_result. "+r.state.processWait.description(), r.state.meta(r.ctxMgr.Status()))
	return true
}

func (r *engineRun) stepOnce() bool {
	if r.ctx.Err() != nil {
		r.log("interrupted: step=%d lastText=%q", r.step, r.state.lastText[:min(len(r.state.lastText), 200)])
		r.result = buildPartialResult(r.state.lastText, r.state.meta(r.ctxMgr.Status()))
		r.err = r.ctx.Err()
		return true
	}

	if r.cfg.PendingUpdates != nil && r.handoffPhase == handoffWorking {
		err := r.cfg.PendingUpdates(func(updates []protocol.BackgroundUpdate) error {
			for _, update := range updates {
				r.ctxMgr.AddTaskUpdate(update.Sequence, update.Text)
			}
			return r.checkpoint("running", "")
		})
		if err != nil {
			r.err = fmt.Errorf("apply task updates: %w", err)
			return true
		}
	}

	profile := r.cfg.Profile
	allowedTools := profile.Tools
	handoffOnly := r.handoffPhase != handoffWorking
	if handoffOnly {
		allowedTools = nil
	}
	ctx := r.ctx
	if r.handoffPhase == handoffFinalizing {
		ctx = modelbudget.WithRole(ctx, modelbudget.Handoff, protocol.ExecutionFromContext(ctx).TaskID+":"+r.cfg.AttemptID)
	}
	if r.executor != nil {
		ctx = execution.WithExecutionState(ctx, r.executor.ExecutionState())
		ctx = core.WithOutputStore(ctx, r.executor.ExecutionState().OutputStore)
		ctx = protocol.WithAccessObservation(ctx, r.executor.AccessObservation())
	}
	r.step++
	output := r.cfg
	output.finalizing = handoffOnly
	calls, text, err := r.engine.reason(ctx, r.ctxMgr, allowedTools, buildSkillWorkflow(r.cfg), r.state.addTokens(r.cfg), r.cfg.RecordLLMUsage, r.cfg.SessionID, r.phase, output)
	r.ctxMgr.SetHints("")
	if protocol.OnlyFailure(err, modelbudget.ErrExhausted) && r.handoffPhase != handoffFinalizing {
		// Another in-flight sibling may consume the last ordinary admission after
		// our boundary check. No request was sent; retry only the handoff boundary.
		r.step--
		return false
	}
	var requestBudgetError *ctxmgr.RequestBudgetError
	if handoffOnly && errors.As(err, &requestBudgetError) {
		r.result = buildProtocolPartialResult(r.state.lastText, "Final handoff context exceeds the model window. Full task history remains saved; no evidence was silently dropped. Resume this task with ordinary budget for compaction or narrower work.", r.state.meta(r.ctxMgr.Status()))
		return true
	}
	if err != nil {
		r.log("error: %v", err)
		r.err = err
		if r.state.lastText != "" {
			r.log("partial_result: %q", r.state.lastText[:min(len(r.state.lastText), 300)])
			r.result = buildPartialResult(r.state.lastText, r.state.meta(r.ctxMgr.Status()))
			return true
		}
		r.result = buildFailedResult(err.Error(), r.state.meta(r.ctxMgr.Status()))
		return true
	}

	if text != "" {
		r.state.lastText = text
	}
	if r.checkpoint != nil {
		if err := r.checkpoint("running", ""); err != nil {
			r.err = fmt.Errorf("save subagent: %w", err)
			return true
		}
	}
	if handoff, submitted, protocolErr := submittedResult(calls); submitted {
		if protocolErr == nil {
			r.complete(handoff)
			r.ctxMgr.AddToolResultsBatch([]ctxmgr.ToolResultMsg{{Message: providertypes.Message{Content: "Result submitted.", ToolCallID: calls[0].ID}, ToolName: submitResultToolName}})
			return true
		}
		r.rejectToolBatch(calls, protocolErr.Error())
		return r.retryRejectedHandoff()
	}
	if len(calls) == 0 {
		if r.handoffPhase == handoffCorrecting {
			return r.retryRejectedHandoff()
		}
		return r.retryCompletionProtocol("response ended without submit_result")
	}
	if r.handoffPhase == handoffCorrecting {
		r.rejectToolBatch(calls, "handoff correction permits only submit_result")
		return r.retryRejectedHandoff()
	}
	if r.handoffPhase == handoffFinalizing {
		reason := r.finalizationReason + " reached; only submit_result is allowed"
		r.rejectToolBatch(calls, reason)
		r.result = buildProtocolPartialResult(r.state.lastText, reason, r.state.meta(r.ctxMgr.Status()))
		r.phase("done")
		return true
	}

	r.state.completionRetries = 0
	r.state.recordCalls(calls)
	if err := r.engine.executeToolBatch(r.ctx, r.cfg, r.ctxMgr, r.executor, calls, r.state, r.phase, r.log); err != nil {
		r.err = err
		r.result = buildFailedResult(err.Error(), r.state.meta(r.ctxMgr.Status()))
		return true
	}
	r.phase("Waiting")
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
			handoff.Outcome, handoff.NextAction, parseErr = optionalHandoffOutcome(call.Args)
		}
		if parseErr == nil {
			handoff.EvidenceIDs, parseErr = optionalEvidenceIDs(call.Args)
		}
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
			handoff.Checks, parseErr = optionalVerificationChecks(call.Args)
		}
		if parseErr == nil {
			handoff.VerificationGaps, parseErr = optionalResultStrings(call.Args, "verification_gaps")
		}
		if parseErr == nil {
			handoff.Unfinished, parseErr = requiredResultStrings(call.Args, "unfinished")
		}
		if parseErr == nil {
			handoff.Risks, parseErr = requiredResultStrings(call.Args, "risks")
		}
		if parseErr == nil {
			parseErr = protocol.ValidateHandoff(handoff.Outcome, handoff.NextAction, handoff.Unfinished)
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

func optionalResultStrings(args map[string]any, name string) ([]string, error) {
	if _, ok := args[name]; !ok {
		return nil, nil
	}
	return requiredResultStrings(args, name)
}

func (r *engineRun) rejectToolBatch(calls []core.ToolCallItem, reason string) {
	results := make([]ctxmgr.ToolResultMsg, len(calls))
	for i, call := range calls {
		call = r.engine.toolRegistry.EnrichCall(call)
		name, _ := call.Effective()
		decision := core.NotExecuted(r.ctx, call, "completion_protocol", reason).NonExecution
		if r.cfg.OnToolCall != nil {
			r.cfg.OnToolCall(ToolCallEvent{Action: protocol.StepActionToolBlocked, CallID: call.ID, ToolName: name, Output: reason, IsError: true, NonExecution: decision})
		}
		results[i] = ctxmgr.ToolResultMsg{
			Message: providertypes.Message{
				Content: reason, ToolCallID: call.ID, IsError: true, NonExecution: decision,
			},
			ToolName: name,
		}
	}
	r.ctxMgr.AddToolResultsBatch(results)
}

func (r *engineRun) retryCompletionProtocol(reason string) bool {
	if r.state.completionRetries >= maxCompletionProtocolRetries {
		r.result = buildProtocolPartialResult(r.state.lastText, reason, r.state.meta(r.ctxMgr.Status()))
		r.phase("done")
		return true
	}
	r.state.completionRetries++
	r.ctxMgr.Add("user",
		"[Sub-agent completion protocol]\nYour response was not submitted. Continue the delegated task, then call "+submitResultToolName+" as the only tool call with every required handoff field. Plain assistant text does not complete the task.",
		providertypes.MessageSourceRuntimeEvent,
	)
	r.phase("Waiting")
	return false
}

func (r *engineRun) retryRejectedHandoff() bool {
	if r.handoffPhase == handoffWorking {
		r.handoffPhase = handoffCorrecting
	}
	if r.state.completionRetries >= maxCompletionProtocolRetries {
		r.result = buildProtocolPartialResult(r.state.lastText, "handoff schema was rejected after correction attempts", r.state.meta(r.ctxMgr.Status()))
		r.phase("done")
		return true
	}
	r.state.completionRetries++
	r.ctxMgr.Add("user",
		"[Sub-agent handoff correction]\nThe preceding tool result names the rejected field. Do not restart or extend the investigation and do not call ordinary tools. Correct only the handoff, then call nekocode_submit_result alone. Preserve the evidence. If a check has no established result, use status=not_run. For a non-complete outcome, provide a concrete next_action and at least one unfinished item. Provide risks as an array; use [] only when assessed and none remain.",
		providertypes.MessageSourceRuntimeEvent,
	)
	r.phase("Waiting")
	return false
}

func (r *engineRun) complete(handoff Handoff) {
	r.phase("done")
	r.result = buildHandoffResult(handoff, r.state.meta(r.ctxMgr.Status()))
	r.log("result: tokens=%d tools=%d duration=%dms output=%q",
		r.result.TotalTokens, r.result.ToolUseCount, r.result.DurationMs,
		r.result.Content[:min(len(r.result.Content), 300)])
}

func (r *engineRun) finish() (*Result, error) {
	if r.result == nil {
		r.result = buildPartialResult(r.state.lastText, r.state.meta(r.ctxMgr.Status()))
	}
	reason := "completed"
	if r.result.Status != StatusCompleted {
		reason = "incomplete"
	}
	if r.handoffPhase == handoffFinalizing {
		reason = r.finalizationReason
	}
	if r.err != nil {
		reason = "execution_error"
	}
	if reason == "process_wait_review" {
		r.result.Content += "\n\n[Coordinator review] " + r.state.processWait.description()
	}
	r.result.Summary = protocol.RunSummary{StepCount: r.step, StopReason: reason}
	return r.result, r.err
}

type Status int

const (
	StatusCompleted Status = iota
	StatusFailed
	StatusPartial
)

type Result struct {
	Summary         protocol.RunSummary
	Status          Status
	Content         string
	TotalTokens     int
	ToolUseCount    int
	DurationMs      int64
	CacheHitTokens  int
	CacheMissTokens int
	Handoff         *Handoff
}

type Handoff = protocol.TaskReport

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
	status := StatusCompleted
	if protocol.HandoffIncomplete(handoff.Outcome, handoff.Unfinished) {
		status = StatusPartial
	}
	result := newResult(status, formatHandoff(handoff), meta)
	copy := handoff
	copy.Checks = append([]VerificationCheck(nil), handoff.Checks...)
	copy.VerificationGaps = append([]string(nil), handoff.VerificationGaps...)
	for i := range copy.Checks {
		copy.Checks[i].Limitations = append([]string(nil), handoff.Checks[i].Limitations...)
	}
	copy.Evidence = append([]string(nil), handoff.Evidence...)
	copy.EvidenceIDs = append([]string(nil), handoff.EvidenceIDs...)
	copy.Files = append([]string(nil), handoff.Files...)
	copy.Unfinished = append([]string(nil), handoff.Unfinished...)
	copy.Risks = append([]string(nil), handoff.Risks...)
	result.Handoff = &copy
	return result
}

func formatHandoff(handoff Handoff) string {
	var out strings.Builder
	out.WriteString(handoff.Summary)
	if handoff.Outcome != "" {
		fmt.Fprintf(&out, "\n\nHandoff outcome: %s\nNext action: %s", handoff.Outcome, handoff.NextAction)
	}
	writeVerificationChecks(&out, handoff.Checks)
	writeHandoffList(&out, "Verification gaps", handoff.VerificationGaps)
	writeHandoffList(&out, "Evidence", handoff.Evidence)
	writeHandoffList(&out, "Execution evidence IDs", handoff.EvidenceIDs)
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

// A persisted request may have been interrupted before tool results committed.
// Close those requests explicitly; never execute them again during recovery.
func closeInterruptedTools(mgr *ctxmgr.Manager, messages []providertypes.Message) {
	answered := make(map[string]bool)
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		switch {
		case msg.Role == "tool":
			answered[msg.ToolCallID] = true
		case msg.Role == "user" && !msg.IsConversationUser():
			continue // A runtime notice may appear between results of a parallel batch.
		case msg.Role == "assistant" && len(msg.ToolCalls) > 0:
			var results []ctxmgr.ToolResultMsg
			for _, call := range msg.ToolCalls {
				if !answered[call.ID] {
					results = append(results, ctxmgr.ToolResultMsg{Message: providertypes.Message{ToolCallID: call.ID, Content: "Previous execution was interrupted. Tool outcome is unknown; inspect current state before proceeding. Do not assume this tool ran or replay it automatically.", IsError: true}, ToolName: call.Function.Name})
				}
			}
			if len(results) > 0 {
				mgr.AddToolResultsBatch(results)
			}
			return
		default:
			return
		}
	}
}
