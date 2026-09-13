package llmstream

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"nekocode/bot/calllog"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider"
	"nekocode/bot/provider/types"
	"nekocode/logger"
)

// StreamCallbacks holds per-token callbacks for ConsumeStream.
type StreamCallbacks struct {
	OnText      func(delta string)
	OnReasoning func(delta string)
	OnPhase     func(phase string)
	AddTokens   func(prompt, completion int)
	OnUsage     func(usage types.StreamUsage)
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
func CallLLM(client provider.LLM, opts LLMCallOptions) (*LLMCallResult, error) {
	start := time.Now()
	tokenCh, errCh := client.ChatStream(opts.Ctx, opts.Messages, opts.ToolDefs)
	if tokenCh == nil {
		err := fmt.Errorf("chat stream failed")
		select {
		case streamErr := <-errCh:
			err = streamErr
		default:
		}
		writeCallRecord(opts, &StreamResult{}, start, err)
		return nil, err
	}

	stream := StreamResult{}
	result, err := func() (*LLMCallResult, error) {
		if err := ConsumeStream(tokenCh, &stream, opts.Callbacks, opts.CheckDone); err != nil {
			go func() { <-errCh }()
			return nil, err
		}

		if opts.CheckDone != nil && opts.CheckDone() {
			go func() { <-errCh }()
			return nil, context.Canceled
		}

		if err := <-errCh; err != nil {
			return nil, err
		}

		return &LLMCallResult{
			Text:               ansiRegex.ReplaceAllString(stream.TextBuf.String(), ""),
			Reasoning:          stream.ReasoningBuf.String(),
			ReasoningSignature: stream.ReasoningSignature,
			ToolCalls:          stream.CollectToolCalls(),
		}, nil
	}()
	if opts.Callbacks.OnUsage != nil && stream.Usage.HasTokens() {
		opts.Callbacks.OnUsage(stream.Usage)
	}
	writeCallRecord(opts, &stream, start, err)
	return result, err
}

// writeRecord is a seam for tests.
var writeRecord = calllog.Write

// writeCallRecord assembles and appends one privacy-safe per-call usage record.
func writeCallRecord(opts LLMCallOptions, stream *StreamResult, start time.Time, callErr error) {
	rec := calllog.Record{
		TS:     time.Now(),
		Source: opts.Source,
		DurMs:  time.Since(start).Milliseconds(),
	}
	rec.SessionID = opts.SessionID
	rec.SetUsage(stream.Usage)
	if fp := stream.Usage.SystemFingerprint; fp != "" {
		rec.SystemFingerprint = calllog.FingerprintID(fp)
	}
	if rec.Source == "" {
		rec.Source = "unknown"
	}
	if !stream.FirstTokenAt.IsZero() {
		rec.TTFTMs = stream.FirstTokenAt.Sub(start).Milliseconds()
	}
	if callErr != nil {
		rec.Err = calllog.ErrorSummary(callErr)
	}
	if meta := stream.Request; meta != nil {
		rec.Model = meta.Model
		rec.Protocol = meta.Protocol
		rec.BaseURL = calllog.SafeBaseURL(meta.BaseURL)
		rec.RequestedEffort = meta.RequestedEffort
		rec.EffectiveEffort = meta.EffectiveEffort
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
func CallLLMWithRetry(ctx context.Context, client provider.LLM, buildOptions func() LLMCallOptions) (*LLMCallResult, error) {
	var result *LLMCallResult
	err := WithRetry(ctx, func() error {
		var err error
		result, err = CallLLM(client, buildOptions())
		return err
	})
	return result, err
}

// WithRetry runs fn under the provider-default retry policy, logging each
// retryable attempt.
func WithRetry(ctx context.Context, fn func() error) error {
	var attempt int
	return provider.Retry(ctx, provider.DefaultRetryConfig, func() error {
		err := fn()
		if err != nil && provider.IsRetryable(err) {
			attempt++
			logger.Log("retry %d: %v", attempt, err)
		}
		return err
	})
}
