package llmstream

import (
	"context"
	"fmt"
	"regexp"

	"nekocode/bot/llm/types"
)

var ansiRegex = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// CallLLM executes a single LLM stream call and returns the result.
func CallLLM(client types.LLM, opts LLMCallOptions) (*LLMCallResult, error) {
	tokenCh, errCh := client.ChatStream(opts.Ctx, opts.Messages, opts.ToolDefs)
	if tokenCh == nil {
		select {
		case err := <-errCh:
			return nil, err
		default:
			return nil, fmt.Errorf("chat stream failed")
		}
	}

	stream := StreamResult{}
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
		Text:      ansiRegex.ReplaceAllString(stream.TextBuf.String(), ""),
		Reasoning: stream.ReasoningBuf.String(),
		ToolCalls: stream.CollectToolCalls(),
	}, nil
}
