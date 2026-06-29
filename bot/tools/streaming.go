package tools

import (
	"nekocode/bot/llm/types"
	"nekocode/bot/tools/llmstream"
)

type StreamCallbacks = llmstream.StreamCallbacks
type StreamResult = llmstream.StreamResult
type ToolAccum = llmstream.ToolAccum
type LLMCallResult = llmstream.LLMCallResult
type LLMCallOptions = llmstream.LLMCallOptions

func ConsumeStream(tokenCh <-chan types.StreamToken, s *StreamResult, cb StreamCallbacks, checkDone func() bool) error {
	return llmstream.ConsumeStream(tokenCh, s, cb, checkDone)
}

func ToLLMToolCalls(calls []ToolCallItem) []types.ToolCall {
	return llmstream.ToLLMToolCalls(calls)
}

func CallLLM(client types.LLM, opts LLMCallOptions) (*LLMCallResult, error) {
	return llmstream.CallLLM(client, opts)
}
