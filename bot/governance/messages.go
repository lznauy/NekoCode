package governance

import "fmt"

const GuardReadOnlySpiral = "You've been reading without acting. Summarize your findings now - don't read any more files."

func ToolResultWarning(count int) string {
	return fmt.Sprintf("%d tool results accumulated. Check for unfinished sub-tasks - if any, continue with task. If all done, call task(verify) to validate, then report results.", count)
}
