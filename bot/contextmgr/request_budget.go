package contextmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"nekocode/bot/contextmgr/token"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider"
	"nekocode/bot/provider/types"
)

// RequestBudgetError describes the final provider-visible request estimate.
// Counts remain heuristic; framing slack and output capacity are reserved
// separately instead of treating the entire window as available input.
type RequestBudgetError struct {
	Window      int
	Messages    int
	Tools       int
	Calibration int
	Output      int
	Safety      int
}

func (e *RequestBudgetError) Total() int {
	return e.Messages + e.Tools + e.Calibration + e.Output + e.Safety
}

func (e *RequestBudgetError) Error() string {
	status := "request budget exceeded"
	if e.Total() <= e.Window {
		status = "request preparation blocked"
	}
	return fmt.Sprintf("%s: estimated %d tokens of %d window (messages=%d, tools=%d, calibration=%d, output reserve=%d, safety=%d); compact history, reduce request content, or select a larger context window",
		status, e.Total(), e.Window, e.Messages, e.Tools, e.Calibration, e.Output, e.Safety)
}

// PrepareRequest is the last context boundary before an inference call. It
// validates the assembled request, compacts at most once, then rebuilds and
// checks again. Transient messages are included without persisting them.
func (m *Manager) PrepareRequest(ctx context.Context, request ModelRequest) ([]types.Message, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		messages, shape := m.projectRequest(request, false)
		m.state.mu.RLock()
		budget, err := estimateRequestBudget(messages, request.Tools, m.state.contextWindow, request.OutputTokens, m.state.reasoning, m.state.vision, m.state.tracker.PromptEstimate())
		autoCompact := attempt == 0 && m.state.compressor != nil && m.state.compressor.shouldAutoCompact(m.state.contextWindow, m.estimatedTokens())
		m.state.mu.RUnlock()
		if err != nil {
			return nil, err
		}
		for _, msg := range messages {
			if msg.Source == core.DeferredOutputSource {
				return nil, fmt.Errorf("%w; %s", budget, core.DeferredOutput)
			}
		}
		// Automatic threshold and final request admission share one attempt.
		// The accepted request is always rebuilt after a history replacement.
		if attempt == 0 && (autoCompact || budget.Total() > budget.Window) && budget.Tools+budget.Output+budget.Safety <= budget.Window {
			applied, err := m.compact(ctx, !autoCompact)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil, err
				}
				return nil, fmt.Errorf("%w; compaction failed: %w", budget, err)
			}
			if applied {
				continue
			}
		}
		if budget.Total() > budget.Window {
			return nil, budget
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		m.state.mu.Lock()
		m.state.prefix.Observe(shape)
		m.state.mu.Unlock()
		return messages, nil
	}
	panic("unreachable request preparation state")
}

func estimateRequestBudget(messages []types.Message, tools []types.ToolDef, window, output int, reasoning types.ReasoningSettings, vision bool, calibrated int) (*RequestBudgetError, error) {
	if window <= 0 {
		window = defaultBudget
	}
	if output <= 0 {
		output = min(4096, max(1, window/4))
	}
	budget := &RequestBudgetError{Window: window, Output: output, Safety: max(32, window/100)}
	budget.Messages = token.EstimateModelTokens(messages, reasoning, vision)
	// Message boundaries and tool-result pairing fields are omitted from the
	// content estimator but still occupy provider framing.
	for _, msg := range messages {
		budget.Messages += 8 + token.EstimateString(msg.ToolCallID)
	}
	if len(tools) > 0 {
		data, err := json.Marshal(tools)
		if err != nil {
			return nil, fmt.Errorf("encode tool definitions for request budget: %w", err)
		}
		budget.Tools = token.EstimateString(string(data)) + 8*len(tools)
	}
	budget.Calibration = max(0, calibrated-budget.Messages-budget.Tools)
	return budget, nil
}

// prepareSummaryRequest validates the formatted summary input without recursively compacting it.
func prepareSummaryRequest(messages []types.Message, archive string, window int, client provider.LLM) ([]types.Message, error) {
	request := buildSummaryMessages(messages, archive)
	if window <= 0 {
		return request, nil
	}
	budget, err := estimateRequestBudget(request, nil, window, provider.MaxOutputTokensFor(client), provider.ReasoningSettingsFor(client), false, 0)
	if err != nil {
		return nil, err
	}
	if budget.Total() > budget.Window {
		return nil, budget
	}
	return request, nil
}
