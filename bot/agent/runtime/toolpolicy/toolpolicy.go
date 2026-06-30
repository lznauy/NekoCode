package toolpolicy

import "strings"

func HasSufficientEditAnchor(args map[string]any) bool {
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

func ExtractTargetPath(toolName string, args map[string]any) string {
	switch toolName {
	case "write", "edit":
		p, _ := args["path"].(string)
		return p
	}
	return ""
}
