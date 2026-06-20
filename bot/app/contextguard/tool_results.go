package contextguard

import (
	"strconv"

	"nekocode/llm/types"
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
		Content: ToolResultWarning(toolResults),
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

func ToolResultWarning(count int) string {
	return "[System] " + strconv.Itoa(count) + " tool results accumulated. Check for unfinished sub-tasks — if any, continue with task. If all done, call task(verify) to validate, then report results."
}
