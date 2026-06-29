package runtime

import "strings"

// isGarbledToolCall detects when a model erroneously serializes tool calls
// into the text content (as XML tags or bare JSON) instead of using the
// structured tool_calls field. Such output is discarded to keep history clean.
func isGarbledToolCall(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if strings.Contains(t, "<invoke") || strings.Contains(t, "</invoke") ||
		strings.Contains(t, "<parameter") || strings.Contains(t, "</parameter") ||
		strings.Contains(t, "<tool_call") || strings.Contains(t, "</tool_call") {
		return true
	}
	return strings.Contains(t, `"tool_calls"`) || strings.Contains(t, `"tool_use"`)
}
