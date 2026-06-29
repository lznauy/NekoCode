package runtime

import (
	"context"
	"time"

	"nekocode/bot/agent/reasoning"
	"nekocode/bot/debug"
	"nekocode/bot/llm/types"
	"nekocode/bot/tools"
)

const synthesizePrompt = "Based on the information collected above, provide a final answer. Do NOT call any more tools. Output your conclusion directly."

func (a *Agent) synthesizeAndReturn(callback RunCallback) *RunResult {
	output := a.forceSynthesize()
	a.ctxMgr.AddAssistantResponse(output, "")
	if callback != nil {
		callback("chat", "", "", output)
	}
	return &RunResult{FinalOutput: output, Steps: a.step}
}

func (a *Agent) forceSynthesize() string {
	// Primary: LLM with retry.
	var text string
	_ = withRetry(a.getCtx(), func() error {
		result, err := a.streamSynthesize(a.getCtx())
		if err != nil {
			return err
		}
		text = result
		return nil
	})
	if text != "" {
		return text
	}

	// Emergency: auto-compact, 30s timeout, no retry, discard garbled.
	debug.Log("forceSynthesize: primary path failed, attempting emergency fallback")
	a.ctxMgr.AutoCompactIfNeeded()
	ctx, cancel := context.WithTimeout(a.getCtx(), 30*time.Second)
	defer cancel()
	if fb, _ := a.streamSynthesize(ctx); fb != "" && !reasoning.IsGarbledToolCall(fb) {
		return fb
	}

	return FallbackSynthesize
}

func (a *Agent) streamSynthesize(ctx context.Context) (string, error) {
	messages := a.ctxMgr.Build(false)
	messages = append(messages, types.Message{Role: "user", Content: synthesizePrompt})

	result, err := tools.CallLLM(a.llmClient, tools.LLMCallOptions{
		Ctx:            ctx,
		Messages:       messages,
		Callbacks:      a.streamCallbacks(),
		EstimatePrompt: true,
	})
	if err != nil {
		return "", err
	}
	return result.Text, nil
}
