package llmstream

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"nekocode/bot/calllog"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/internal/modelretry"
	"nekocode/bot/provider/llm"
	"nekocode/bot/provider/types"
	"nekocode/logger"
	"nekocode/util/modelbudget"
)

// StreamCallbacks holds per-token callbacks for ConsumeStream.
type StreamCallbacks struct {
	OnText      func(delta string)
	OnReasoning func(delta string)
	OnPhase     func(phase string)
	AddTokens   func(prompt, completion int)
	OnUsage     func(usage types.StreamUsage)
	// OnTextReset retracts the current response when an attempt is replaced.
	OnTextReset func()
}

// StreamResult accumulates the output of consuming a ChatStream.
type StreamResult struct {
	TextBuf            strings.Builder
	ReasoningBuf       strings.Builder
	ReasoningSignature string
	TcAccum            map[int]*ToolAccum
	Usage              types.StreamUsage
	Request            *types.RequestMeta
	FirstTokenAt       time.Time
}

// ToolAccum accumulates incremental tool call deltas for a single tool call.
type ToolAccum struct {
	ID   string
	Name string
	Args strings.Builder
}

// LLMCallResult holds the result of a single LLM stream call.
type LLMCallResult struct {
	Text               string
	Reasoning          string
	ReasoningSignature string
	ToolCalls          []core.ToolCallItem
}

// LLMCallOptions configures a single LLM call.
type LLMCallOptions struct {
	Request   types.RequestOptions
	Ctx       context.Context
	Messages  []types.Message
	ToolDefs  []types.ToolDef
	Callbacks StreamCallbacks
	CheckDone func() bool
	// Source labels the call origin ("main", "synthesize", "subagent") in
	// the evidence log; Diagnostics reports the request's prefix fingerprint.
	Source      string
	SessionID   string
	Diagnostics func() calllog.PrefixDiag
}

var ansiRegex = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// CallLLM executes a single LLM stream call and returns the result. Every
// call — success or failure — leaves one structured record in the evidence
// log (calllog.Write).
func CallLLM(client llm.Client, opts LLMCallOptions) (*LLMCallResult, error) {
	if err := modelbudget.Admit(opts.Ctx); err != nil {
		return nil, err
	}

	start := time.Now()
	requestCtx, cancel := context.WithCancel(opts.Ctx)
	defer cancel()
	tokenCh, errCh := llm.ChatStream(client, requestCtx, opts.Messages, opts.ToolDefs, opts.Request)
	if tokenCh == nil {
		err := fmt.Errorf("chat stream failed")
		select {
		case streamErr := <-errCh:
			if streamErr != nil {
				err = streamErr
			}
		default:
		}
		writeCallRecord(opts, &StreamResult{}, start, err, "request")
		return nil, err
	}

	stream := StreamResult{}
	failureStage := "completion"
	err := ConsumeStream(requestCtx, tokenCh, errCh, &stream, opts.Callbacks, opts.CheckDone)
	if errors.Is(err, llm.ErrStreamIdle) {
		failureStage = "stream"
	} else if err != nil && (requestCtx.Err() != nil || errors.Is(err, errAttemptStopped)) {
		failureStage = "cancelled"
	}
	// Preserve partial output so a replacement attempt can retract its preview.
	result := &LLMCallResult{
		Text:               ansiRegex.ReplaceAllString(stream.TextBuf.String(), ""),
		Reasoning:          stream.ReasoningBuf.String(),
		ReasoningSignature: stream.ReasoningSignature,
	}
	if err == nil {
		result.ToolCalls, err = stream.CollectToolCalls()
	}
	if opts.Callbacks.OnUsage != nil && stream.Usage.HasTokens() {
		opts.Callbacks.OnUsage(stream.Usage)
	}
	writeCallRecord(opts, &stream, start, err, failureStage)
	return result, err
}

// writeRecord is a seam for tests.
var writeRecord = calllog.Write

// writeCallRecord assembles and appends one privacy-safe per-call usage record.
func writeCallRecord(opts LLMCallOptions, stream *StreamResult, start time.Time, callErr error, failureStage ...string) {
	rec := calllog.NewRecord(opts.Ctx, opts.Source, opts.SessionID, start)
	rec.SetOutcome(stream.Usage, stream.Request, callErr)
	modelbudget.FromContext(opts.Ctx).ObserveTokens(rec.TotalTokens, *rec.UsageReported)
	if callErr != nil && len(failureStage) > 0 {
		rec.FailureStage = failureStage[0]
	}
	if !stream.FirstTokenAt.IsZero() {
		rec.TTFTMs = stream.FirstTokenAt.Sub(start).Milliseconds()
	}
	if opts.Diagnostics != nil {
		if diag := opts.Diagnostics(); !diag.IsZero() {
			rec.PrefixDiag = &diag
		}
	}
	writeRecord(rec)
}

// CallLLMWithRetry calls CallLLM, rebuilding the call options on every
// retryable failure via buildOptions.
func CallLLMWithRetry(ctx context.Context, client llm.Client, buildOptions func() LLMCallOptions) (*LLMCallResult, error) {
	var result *LLMCallResult
	var lastOptions LLMCallOptions
	err := WithRetry(ctx, func() error {
		opts := buildOptions()
		lastOptions = opts
		if result != nil && result.Text != "" && opts.Callbacks.OnTextReset != nil {
			opts.Callbacks.OnTextReset()
		}
		var err error
		result, err = CallLLM(client, opts)
		return err
	})
	if err != nil {
		return result, err
	}
	// Reasoning-only retry: GLM-style thinking models occasionally end a
	// stream with reasoning but no content and finish=stop — a "successful"
	// call, so the network-level retry never fires, and the turn collapses
	// into an empty answer. One immediate retry with thinking disabled gets
	// a straight answer. Only models declaring a thinking switch take this
	// path; without the profile the disabled request carries no switch and
	// the retry would be a pointless duplicate call.
	if retryEmptyContent(client, result) && (lastOptions.Request.DisableThinking == nil || !*lastOptions.Request.DisableThinking) {
		logger.Log("llmstream: reasoning-only empty response, retrying once with thinking disabled")
		retryOpts := buildOptions()
		disabled := true
		retryOpts.Request.DisableThinking = &disabled
		retry, retryErr := CallLLM(client, retryOpts)
		if retryErr != nil {
			if retry != nil && retry.Text != "" && retryOpts.Callbacks.OnTextReset != nil {
				retryOpts.Callbacks.OnTextReset()
			}
			return result, retryErr
		}
		return retry, nil
	}
	return result, nil
}

// retryEmptyContent reports whether result is a completed reasoning-only
// response worth one thinking-disabled retry.
func retryEmptyContent(client llm.Client, result *LLMCallResult) bool {
	if result == nil || client == nil || !llm.SupportsRequestOptions(client) {
		return false
	}
	settings := llm.ReasoningSettingsFor(client)
	if settings.ThinkingMode == "" || settings.Disabled {
		return false
	}
	return strings.TrimSpace(result.Text) == "" &&
		strings.TrimSpace(result.Reasoning) != "" &&
		len(result.ToolCalls) == 0
}

// WithRetry runs fn under the provider-default retry policy, logging each
// retryable attempt.
func WithRetry(ctx context.Context, fn func() error) error {
	var attempt int
	return modelretry.Retry(ctx, modelretry.Default(), func() error {
		err := fn()
		if err != nil && modelretry.IsRetryable(err) {
			attempt++
			logger.Log("retry %d: %v", attempt, err)
		}
		return err
	})
}
