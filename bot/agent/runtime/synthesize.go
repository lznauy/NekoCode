package runtime

import (
	"context"
	"errors"
	"time"

	"nekocode/bot/debug"
	"nekocode/bot/tools"
	"nekocode/llm/types"
)

const synthesizePrompt = "Based on the information collected above, provide a final answer. Do NOT call any more tools. Output your conclusion directly."

func (a *Agent) forceSynthesize() string {
	if text := a.trySynthesize(); text != "" {
		return text
	}
	debug.Log("forceSynthesize: primary path failed, attempting emergency fallback")
	if fb := a.emergencySynthesize(); fb != "" {
		return fb
	}
	return "Unable to produce a final summary — the model is currently unavailable. The task's tool operations may have completed, but the results could not be synthesized. Please try again or check the conversation log for details."
}

func (a *Agent) trySynthesize() string {
	var text string
	err := withRetry(a.getCtx(), func() error {
		result, err := a.streamSynthesize(a.getCtx())
		if err != nil {
			return err
		}
		text = result
		return nil
	})
	if err != nil && errors.Is(err, context.Canceled) {
		return ""
	}
	return text
}

func (a *Agent) emergencySynthesize() string {
	a.ctxMgr.AutoCompactIfNeeded()

	ctx, cancel := context.WithTimeout(a.getCtx(), 30*time.Second)
	defer cancel()

	text, _ := a.streamSynthesize(ctx)
	if isGarbledToolCall(text) {
		return ""
	}
	return text
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
