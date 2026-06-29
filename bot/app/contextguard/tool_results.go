package contextguard

import (
	"nekocode/bot/agent/runtime"
	"nekocode/bot/llm/types"
)

const (
	DefaultToolResultThreshold = 40
	DefaultWarnInterval        = 10
)

func ApplyToolResultGuardrail(messages []types.Message, lastWarned *int) []types.Message {
	return ApplyToolResultGuardrailWithLimits(messages, lastWarned, DefaultToolResultThreshold, DefaultWarnInterval)
}

func ApplyToolResultGuardrailWithLimits(messages []types.Message, lastWarned *int, threshold, interval int) []types.Message {
	toolResults := CountToolResults(messages)
	if lastWarned == nil || toolResults <= threshold || toolResults-*lastWarned < interval {
		return messages
	}
	*lastWarned = toolResults
	return append(messages, types.Message{
		Role:    "user",
		Content: runtime.ToolResultWarning(toolResults),
		Source:  "system",
	})
}

func CountToolResults(messages []types.Message) int {
	count := 0
	for _, m := range messages {
		if m.Role == "tool" {
			count++
		}
	}
	return count
}

