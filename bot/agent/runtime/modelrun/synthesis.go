package modelrun

import (
	"context"
	"time"

	"nekocode/bot/agent/runtime/messages"
	"nekocode/bot/agent/runtime/reasoning"
	"nekocode/bot/debug"
	"nekocode/bot/llm/types"
	"nekocode/bot/tools"
)

const synthesizePrompt = "Based on the information collected above, provide a final answer. Do NOT call any more tools. Output your conclusion directly."

func (r *Runner) Synthesize() string {
	output := r.forceSynthesize()
	r.host.ContextManager().AddAssistantResponse(output, "")
	return output
}

func (r *Runner) forceSynthesize() string {
	var text string
	_ = withRetry(r.host.Context(), func() error {
		result, err := r.streamSynthesize(r.host.Context())
		if err != nil {
			return err
		}
		text = result
		return nil
	})
	if text != "" {
		return text
	}

	debug.Log("forceSynthesize: primary path failed, attempting emergency fallback")
	r.host.ContextManager().AutoCompactIfNeeded()
	ctx, cancel := context.WithTimeout(r.host.Context(), 30*time.Second)
	defer cancel()
	if fb, _ := r.streamSynthesize(ctx); fb != "" && !reasoning.IsGarbledToolCall(fb) {
		return fb
	}

	return messages.FallbackSynthesize
}

func (r *Runner) streamSynthesize(ctx context.Context) (string, error) {
	messages := r.host.ContextManager().Build(false)
	messages = append(messages, types.Message{Role: "user", Content: synthesizePrompt})

	result, err := tools.CallLLM(r.host.LLM(), tools.LLMCallOptions{
		Ctx:       ctx,
		Messages:  messages,
		Callbacks: r.streamCallbacks(),
	})
	if err != nil {
		return "", err
	}
	return result.Text, nil
}
