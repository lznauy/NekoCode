package ledger

import "strings"

type FinalIssue struct {
	Type    string
	Message string
}

func CheckFinalAnswer(answer string, snap Snapshot) []FinalIssue {
	var issues []FinalIssue
	text := strings.ToLower(answer)
	if snap.HasNonDocumentationModifications() && !snap.HasPassingVerification() && !mentionsUnverified(text) {
		issues = append(issues, FinalIssue{
			Type:    "missing_verification",
			Message: "你已经修改了文件，但还没有成功验证。请先运行合适的验证命令；如果无法验证，最终回答必须明确说明未验证。",
		})
	}
	if claimsTestsPassed(text) && !snap.HasPassingVerification() {
		issues = append(issues, FinalIssue{
			Type:    "unsupported_test_claim",
			Message: "最终回答声称测试或验证已通过，但 ledger 中没有成功验证记录。请运行验证命令，或移除该声明。",
		})
	}
	if len(snap.ToolErrors) > 0 && claimsCompleted(text) && !mentionsFailure(text) {
		issues = append(issues, FinalIssue{
			Type:    "unreported_tool_error",
			Message: "存在工具错误，但最终回答没有披露。请处理错误或在最终回答中说明。",
		})
	}
	return issues
}

func FormatIssues(issues []FinalIssue) string {
	if len(issues) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Final answer blocked by governance checks:\n")
	for _, issue := range issues {
		b.WriteString("- ")
		b.WriteString(issue.Message)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func mentionsUnverified(text string) bool {
	return strings.Contains(text, "未验证") ||
		strings.Contains(text, "没有验证") ||
		strings.Contains(text, "unable to verify") ||
		strings.Contains(text, "not verified") ||
		strings.Contains(text, "could not verify")
}

func claimsTestsPassed(text string) bool {
	return strings.Contains(text, "测试通过") ||
		strings.Contains(text, "验证通过") ||
		strings.Contains(text, "go test") && strings.Contains(text, "pass") ||
		strings.Contains(text, "tests pass") ||
		strings.Contains(text, "tests passed") ||
		strings.Contains(text, "verification passed")
}

func claimsCompleted(text string) bool {
	return strings.Contains(text, "已完成") ||
		strings.Contains(text, "完成了") ||
		strings.Contains(text, "已修复") ||
		strings.Contains(text, "fixed") ||
		strings.Contains(text, "completed")
}

func mentionsFailure(text string) bool {
	return strings.Contains(text, "失败") ||
		strings.Contains(text, "错误") ||
		strings.Contains(text, "failed") ||
		strings.Contains(text, "error")
}
