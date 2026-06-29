package governance

import "fmt"

const GuardReadOnlySpiral = "You've been reading without acting. Summarize your findings now - don't read any more files."

func ToolResultWarning(count int) string {
	return fmt.Sprintf("%d tool results accumulated. Check for unfinished sub-tasks - if any, continue with task. If all done, call task(verify) to validate, then report results.", count)
}

func ReadBeforeWriteWarning(targetPath string) string {
	return "你正在修改 " + targetPath + "，但 ledger 中没有该文件的读取记录。请先 Read 确认当前内容，确认差异后再 edit/write。"
}
