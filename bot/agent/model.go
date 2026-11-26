package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"nekocode/bot/agent/internal/llmstream"
	"nekocode/bot/agent/internal/toolpolicy"
	"nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/policy"
	"nekocode/bot/provider/llm"
	"nekocode/bot/provider/types"
	"nekocode/logger"
	"nekocode/protocol"
	"nekocode/util/modelbudget"
)

const synthesizePrompt = "Based on the information collected above, provide a final answer. Do NOT call any more tools. Output your conclusion directly. Explicitly report unfinished todo steps and blockers; a limit is not task completion."

const fallbackSynthesize = "Unable to produce a final summary — the model is currently unavailable. The task's tool operations may have completed, but the results could not be synthesized. Please try again or check the conversation log for details."

// modelRunner wraps the LLM call for a turn: streaming, retry, response
// classification, and final-answer synthesis.
type modelRunner struct {
	agent *Agent
}

func newModelRunner(agent *Agent) *modelRunner {
	return &modelRunner{agent: agent}
}

func (r *modelRunner) reason(input string) *reasoningResult {
	a := r.agent
	a.stream.emitPhase(protocol.PhaseThinking)

	ctx := a.getCtx()
	result, err := r.callLLMForTool(ctx, input)
	if err != nil {
		a.stream.discardText()
		if errors.Is(err, context.Canceled) && (ctx.Err() != nil || a.life.Finished().Load() || a.run.stopReason == policy.StopInterrupted) {
			return &reasoningResult{Interrupted: true}
		}
		a.run.err = err
		return &reasoningResult{}
	}
	if len(result.ToolCalls) == 0 && strings.TrimSpace(result.Text) == "" {
		a.stream.discardText()
		return &reasoningResult{Action: actionEmptyResponse}
	}
	reasoning := fromLLM(result.ToolCalls, result.Text)
	if reasoning.GarbledToolCall {
		a.stream.discardText()
	}
	reasoning.ReasoningContent = result.Reasoning
	reasoning.ReasoningSignature = result.ReasoningSignature
	return reasoning
}

// This hint repeats a literal from the request. Only read determines whether it exists.
// An explicit directory distinguishes an arbitrary filename from a version or host.
// For bare names, restrict this optional hint to familiar project file types.
var projectFileSuffix = regexp.MustCompile(`(?i)[.](go|mod|sum|py|js|jsx|ts|tsx|rs|java|kt|c|cc|cpp|h|hpp|cs|rb|php|sh|bash|sql|html|css|vue|json|jsonl|ya?ml|toml|ini|xml|csv|tsv|md|txt|log)$`)
var relativeFile = regexp.MustCompile(`^([[:alnum:]_-]+/)*[[:alnum:]_-]+([.][[:alnum:]_-]+)+$`)

func mentionedFile(input string) string {
	if len(input) > 8192 {
		return ""
	}
	var candidate string
	for _, word := range strings.FieldsFunc(input, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("，。！？、；：‘’“”「」（）()`\"'", r)
	}) {
		path := strings.TrimPrefix(word, "./")
		if len(path) > 240 || !relativeFile.MatchString(path) {
			continue
		}
		if !strings.ContainsRune(word, '/') && !projectFileSuffix.MatchString(path) {
			continue
		}
		if candidate != "" && candidate != path {
			return ""
		}
		candidate = path
	}
	return candidate
}

func (r *modelRunner) callLLMForTool(ctx context.Context, input string) (*llmstream.LLMCallResult, error) {
	a := r.agent
	toolDefs := core.ToToolDefs(a.deps.toolExecutor.ToolDescriptors())
	policyHints, err := r.preModelHints(ctx)
	if err != nil {
		return nil, err
	}
	ctx = protocol.WithPolicyConfiguration(ctx, a.deps.gov.Configuration())
	ctx = protocol.WithProfileConfiguration(ctx, "")
	ctx = protocol.WithAccessObservation(ctx, a.deps.toolExecutor.AccessObservation())
	reminder := a.todoProgressReminder()
	hasTodoReminder := len(reminder) > 0
	if remaining := maxAgentSteps - a.run.step; remaining == 20 || remaining == 5 {
		reminder = append(reminder, types.Message{Role: "user", Source: types.MessageSourceHint,
			Content: fmt.Sprintf("This run has %d working steps left, including this one. Use the remaining steps for the user's outstanding requirements and verification. Report any unfinished work accurately.", remaining)})
	}
	if a.run.step == 0 && !protocol.CoordinationFromContext(ctx).Notification && a.deps.toolExecutor.CheckToolPolicy("read") == nil && a.deps.toolRegistry != nil && a.deps.toolRegistry.Has("read") {
		if path := mentionedFile(input); path != "" {
			reminder = append(reminder, types.Message{Role: "user", Source: types.MessageSourceHint, Content: fmt.Sprintf("User-supplied path: %q. If inspecting this file, call read with this exact path first. Locate it with glob only if read reports it missing.", path)})
		}
	}
	messages, err := a.deps.ctxMgr.PrepareRequest(ctx, contextmgr.ModelRequest{TaskReferences: r.taskReferences(ctx), Tools: toolDefs, PolicyHints: policyHints, ExtraMessages: reminder, OutputTokens: llm.MaxOutputTokensFor(a.deps.llmClient)})
	if err != nil {
		return nil, err
	}
	toolpolicy.ObserveReadContext(execution.WithExecutionState(ctx, a.deps.toolExecutor.ExecutionState()), messages)

	result, err := llmstream.CallLLMWithRetry(ctx, a.deps.llmClient, func() llmstream.LLMCallOptions {
		return llmstream.LLMCallOptions{
			Ctx:         ctx,
			Messages:    messages,
			ToolDefs:    toolDefs,
			Callbacks:   r.streamCallbacks(),
			CheckDone:   a.life.Finished().Load,
			Source:      "main",
			SessionID:   a.sessionID(),
			Diagnostics: a.deps.ctxMgr.PrefixDiagnostics,
		}
	})
	if err != nil {
		return nil, err
	}

	if hasTodoReminder {
		a.run.todoBatches = 0
	}

	if len(result.ToolCalls) > 0 {
		if isGarbledToolCall(result.Text) || garbledPrefix(result.Text) {
			result.Text = ""
			a.stream.discardText()
		}
		a.deps.ctxMgr.AddAssistant(types.Message{Role: "assistant", Content: result.Text,
			ReasoningContent: result.Reasoning, ReasoningSignature: result.ReasoningSignature,
			ToolCalls: llmstream.ToLLMToolCalls(result.ToolCalls)})
	}
	return result, nil
}

// synthesize produces a final answer when the run ended without one (step
// limit, repeated failures): it asks the LLM once for a tool-free summary
// and persists it. On failure or garbled output it falls back to a fixed
// message — no retries: this path fires precisely when the model is already
// misbehaving, so extra calls rarely help. The fallback itself is not
// persisted: it is not a model response, and recording it as one would mark
// an unanswered run as answered, stranding the user message in the context.
func (r *modelRunner) synthesize() string {
	a := r.agent
	if a.run.err != nil {
		return ""
	}
	ctx := a.getCtx()
	text, err := r.streamSynthesize(ctx)
	if a.run.err != nil {
		a.stream.discardText()
		return ""
	}
	usable := err == nil && usableFinalText(text)
	garbled := isGarbledToolCall(text)
	if ctx.Err() != nil || a.run.stopReason == policy.StopInterrupted {
		a.stream.discardText()
		a.run.stopReason = policy.StopInterrupted
		return ""
	}
	if !usable {
		a.stream.discardText()
		if errors.Is(err, context.Canceled) || errors.Is(err, modelbudget.ErrExhausted) {
			a.run.err = err
			return ""
		}
		var budgetErr *contextmgr.RequestBudgetError
		if !errors.Is(err, context.DeadlineExceeded) && (errors.As(err, &budgetErr) || errors.Is(err, protocol.ErrIncompleteResponse)) {
			a.run.err = err
			return ""
		}
		logger.Log("synthesize: LLM summary failed (err=%v), using fallback", err)
		text = fallbackSynthesize
	}
	if a.deps.gov != nil {
		intent := policy.FinalIntentFinal
		if !usable {
			intent = policy.FinalIntentError
		}
		out := policy.Resolve(a.deps.gov.BeforeStop(policy.TurnResult{Intent: intent, Text: text, Garbled: garbled}, a.getCtx()))
		// This is the terminal recovery path: no further generation or tool retries.
		// Advisory hints remain observable; rejection must never become a fallback.
		if out.Action == policy.Reject || (out.Action == policy.Terminate && out.StopReason == policy.StopPolicyBlocked) {
			a.run.err = fmt.Errorf("final synthesis rejected by hook: %s", out.Reason)
			a.run.stopReason = policy.StopPolicyBlocked
			a.stream.discardText()
			a.clearFinalState()
			return ""
		}
		if out.Action == policy.Terminate {
			a.run.hookStopped = true
			a.run.stopReason = out.StopReason
			if out.StopReason == policy.StopInterrupted {
				a.stream.discardText()
				return ""
			}
		}
	}
	a.turnRunner.reviewPreservedFiles(&text, false)
	text += a.turnRunner.todoDispositionSummary(true)
	if usable {
		a.deps.ctxMgr.AddAssistant(types.Message{Role: "assistant", Content: text})
		a.run.finalText = text
		a.run.finalPersisted = true
	}
	return text
}

// usableFinalText reports whether text is a non-empty, non-garbled final answer.
func usableFinalText(text string) bool {
	return strings.TrimSpace(text) != "" && !isGarbledToolCall(text)
}

func (r *modelRunner) streamSynthesize(ctx context.Context) (string, error) {
	a := r.agent
	policyHints, err := r.preModelHints(ctx)
	if err != nil {
		return "", err
	}
	ctx = protocol.WithPolicyConfiguration(ctx, a.deps.gov.Configuration())
	ctx = protocol.WithProfileConfiguration(ctx, "")
	ctx = protocol.WithAccessObservation(ctx, a.deps.toolExecutor.AccessObservation())
	messages, err := a.deps.ctxMgr.PrepareRequest(ctx, contextmgr.ModelRequest{TaskReferences: r.taskReferences(ctx), PolicyHints: policyHints, OutputTokens: llm.MaxOutputTokensFor(a.deps.llmClient), ExtraMessages: []types.Message{{Role: "user", Content: synthesizePrompt}}})
	if err != nil {
		return "", err
	}

	result, err := llmstream.CallLLM(a.deps.llmClient, llmstream.LLMCallOptions{
		Ctx:         ctx,
		Messages:    messages,
		Callbacks:   r.streamCallbacks(),
		Source:      "synthesize",
		SessionID:   a.sessionID(),
		Diagnostics: a.deps.ctxMgr.PrefixDiagnostics,
	})
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

func (r *modelRunner) preModelHints(ctx context.Context) (string, error) {
	gov := r.agent.deps.gov
	if gov == nil {
		return "", nil
	}
	out := policy.Resolve(gov.BeforeModel(ctx))
	if out.StopReason == policy.StopInterrupted {
		if ctx.Err() == nil {
			r.agent.run.stopReason = policy.StopInterrupted
			r.agent.life.Finished().Store(true)
		}
		return "", context.Canceled
	}
	if out.Action != policy.Continue {
		r.agent.run.err = fmt.Errorf("model request rejected by hook: %s", out.Reason)
		return "", r.agent.run.err
	}
	hints := out.Hints
	if len(hints) == 0 {
		return "", nil
	}
	return policy.FormatHints(hints), nil
}

func (r *modelRunner) streamCallbacks() llmstream.StreamCallbacks {
	a := r.agent
	return llmstream.StreamCallbacks{
		OnText: func(delta string) {
			a.stream.emitText(delta)
		},
		OnTextReset: func() {
			a.stream.discardText()
		},
		OnReasoning: func(delta string) {
			a.stream.emitReasoning(delta)
		},
		OnPhase: func(phase string) {
			a.stream.emitPhase(phase)
		},
		AddTokens: func(prompt, completion int) {
			a.AddCompletionTokens(completion)
		},
		OnUsage: func(usage types.StreamUsage) {
			a.deps.ctxMgr.RecordModelUsage(usage)
			a.RecordLLMUsage(usage)
		},
	}
}

type actionType int

const (
	actionChat actionType = iota
	actionExecuteTool
	actionEmptyResponse
)

type reasoningResult struct {
	Action             actionType
	ActionInput        string
	ToolCalls          []core.ToolCallItem
	TextContent        string
	ReasoningContent   string
	ReasoningSignature string
	Interrupted        bool
	GarbledToolCall    bool
}

func fromLLM(toolCalls []core.ToolCallItem, text string) *reasoningResult {
	if len(toolCalls) > 0 {
		return &reasoningResult{Action: actionExecuteTool, ToolCalls: toolCalls, TextContent: text}
	}
	if isGarbledToolCall(text) {
		logger.Log("GarbledToolCall: XML leaked (len=%d)", len(text))
		return &reasoningResult{Action: actionChat, GarbledToolCall: true}
	}
	return &reasoningResult{Action: actionChat, ActionInput: text}
}

func (r *modelRunner) taskReferences(ctx context.Context) contextmgr.TaskReferences {
	a := r.agent
	if a.deps.taskReferences == nil {
		return contextmgr.TaskReferences{}
	}
	sessionID := a.sessionID()
	if cached := a.run.taskReferences; cached != nil && cached.SessionID == sessionID {
		return *cached
	}
	refs := a.deps.taskReferences(ctx, sessionID)
	if !refs.Unavailable && ctx.Err() == nil {
		// Keep only the bounded request view, never source metadata or task bodies.
		refs.Items = append([]contextmgr.TaskReference(nil), refs.Items[:min(len(refs.Items), contextmgr.MaxTaskReferences)]...)
		a.run.taskReferences = &refs
	}
	return refs
}
