package runtime

import "nekocode/bot/agent/reasoning"

func isGarbledToolCall(text string) bool {
	return reasoning.IsGarbledToolCall(text)
}
