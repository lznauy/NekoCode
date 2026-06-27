package runtime

import (
	"os"
	"strings"

	"nekocode/bot/tools"
)

func (a *Agent) preEditBlockReason(tc tools.ToolCallItem) string {
	if a.gov == nil || a.gov.Ledger == nil {
		return ""
	}
	if tc.Name != "edit" && tc.Name != "write" {
		return ""
	}
	targetPath := extractTargetPath(tc.Name, tc.Args)
	if targetPath == "" || a.gov.Ledger.WasRead(targetPath) {
		return ""
	}
	if tc.Name == "edit" && hasSufficientEditAnchor(tc.Args) {
		return ""
	}
	resolved, err := tools.ValidatePath(targetPath)
	if err != nil {
		return ""
	}
	if _, err := os.Stat(resolved); err != nil {
		return ""
	}
	return "你正在修改 " + targetPath + "，但 ledger 中没有该文件的读取记录。请先 Read 确认当前内容，确认差异后再 edit/write。"
}

func hasSufficientEditAnchor(args map[string]any) bool {
	oldString, _ := args["oldString"].(string)
	oldString = strings.TrimSpace(oldString)
	if oldString == "" {
		return false
	}
	if len([]rune(oldString)) >= 200 {
		return true
	}
	lines := strings.Split(oldString, "\n")
	nonEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty++
		}
	}
	return nonEmpty >= 5
}

func extractTargetPath(toolName string, args map[string]any) string {
	switch toolName {
	case "write", "edit":
		p, _ := args["path"].(string)
		return p
	}
	return ""
}
