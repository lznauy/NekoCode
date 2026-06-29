package contextmgr

import (
	"fmt"

	"nekocode/bot/llm/types"
)

const (
	DefaultToolResultThreshold = 40
	DefaultWarnInterval        = 10
)

type ToolResultGuardrailOptions struct {
	LastWarned *int
	Threshold  int
	Interval   int
	Warning    func(int) string
}

func ApplyToolResultGuardrail(messages []types.Message, opts ToolResultGuardrailOptions) []types.Message {
	threshold := opts.Threshold
	if threshold == 0 {
		threshold = DefaultToolResultThreshold
	}
	interval := opts.Interval
	if interval == 0 {
		interval = DefaultWarnInterval
	}

	toolResults := CountToolResults(messages)
	if opts.LastWarned == nil || toolResults <= threshold || toolResults-*opts.LastWarned < interval {
		return messages
	}
	*opts.LastWarned = toolResults

	warning := defaultToolResultWarning
	if opts.Warning != nil {
		warning = opts.Warning
	}
	return append(messages, types.Message{
		Role:    "user",
		Content: warning(toolResults),
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

func defaultToolResultWarning(count int) string {
	return fmt.Sprintf("%d tool results accumulated; consider summarizing or narrowing the task.", count)
}
