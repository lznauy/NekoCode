package semantics

import "nekocode/bot/tools/core"

// IsAllExploratory returns true when every call in the batch is read-only
// information gathering.
func IsAllExploratory(calls []core.ToolCallItem) bool {
	if len(calls) == 0 {
		return false
	}
	for _, c := range calls {
		switch c.Name {
		case "read", "grep", "glob", "list", "web_search", "web_fetch":
			continue
		default:
			return false
		}
	}
	return true
}
