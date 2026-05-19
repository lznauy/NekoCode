package compact

// Layer 1: Tool Result Budgeting.
// Only grep results are truncated (can produce massive output).

const budgetNav = 2000

// BudgetResult truncates a tool result before storage.
// Returns the (possibly truncated) content and whether truncation occurred.
func BudgetResult(content string, toolName string) (string, bool) {
	limit := budgetLimit(toolName)
	if limit <= 0 || len(content) <= limit {
		return content, false
	}
	// Cut at a newline boundary when possible.
	cut := limit
	if nl := lastIndexByte(content[:cut], '\n'); nl > cut/2 {
		cut = nl
	}
	compactLog("budget_result: truncated %s result from %d to %d chars", toolName, len(content), cut)
		return content[:cut] + "\n[... truncated]", true
}

func budgetLimit(toolName string) int {
	if toolName == "grep" {
		return budgetNav
	}
	return 0
}

func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}
