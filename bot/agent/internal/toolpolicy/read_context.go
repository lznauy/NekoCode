package toolpolicy

import (
	"context"

	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/provider/types"
)

// ObserveReadContext binds write preconditions to the actual model request,
// not to successful tool execution or the user-visible transcript.
func ObserveReadContext(ctx context.Context, messages []types.Message) {
	deliveries := make(map[string]string)
	for _, message := range messages {
		if message.Role == "tool" && !message.IsError && message.ReadID != "" {
			deliveries[message.ReadID] = message.Content
		}
	}
	execution.FileReadsFromContext(ctx).Observe(deliveries)
}
