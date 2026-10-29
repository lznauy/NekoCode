package connect

import (
	"fmt"
	"strings"

	controlruntime "nekocode/runtime"
)

// ApprovalDetail is one transport-neutral fact the user must see before
// deciding. Rich and plain-text connectors render the same facts with their
// own markup.
type ApprovalDetail struct {
	Label string
	Value string
}

// ApprovalDetails extracts security-relevant context from an approval card.
func ApprovalDetails(p controlruntime.ApprovalView) []ApprovalDetail {
	var details []ApprovalDetail
	add := func(label, value string) {
		if value = strings.TrimSpace(value); value != "" {
			details = append(details, ApprovalDetail{Label: label, Value: value})
		}
	}
	if p.Approval == nil {
		return details
	}
	approvalReason := p.Approval.Risk
	permissionReason := p.Approval.Reason
	add("风险", friendlyApprovalReason(approvalReason))
	if permissionReason != approvalReason {
		add("原因", friendlyApprovalReason(permissionReason))
	}
	add("动态结构", friendlyStructureList(p.Approval.Structures))
	if len(p.Approval.Capabilities) > 0 {
		add("权限", friendlyCapabilityList(strings.Join(p.Approval.Capabilities, ", ")))
	}
	if scope := string(p.Approval.Scope); scope != "" {
		switch scope {
		case "once":
			scope = "仅当前调用"
		case "project":
			scope = "当前项目"
		}
		add("范围", scope)
	}
	if p.Approval.Workspace != "" {
		add("工作区", p.Approval.Workspace)
	}
	if p.Approval.OutboundDomains != nil {
		domains := strings.Join(p.Approval.OutboundDomains, "、")
		if domains == "" {
			domains = "禁止联网"
		}
		add("网络域名", domains)
	}
	add("可写目录", strings.Join(p.Approval.WritePaths, "、"))
	return details
}

// ApprovalText is the canonical plain-text rendering of a pending approval,
// used directly by text-only channels and as the fallback when a rich card
// cannot be delivered.
func ApprovalText(p controlruntime.ApprovalView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "需要审批: %s", p.ToolName)
	if cmd, ok := p.Args["command"].(string); ok && cmd != "" {
		fmt.Fprintf(&b, "\n%s", TruncateRunes(cmd, 600))
	} else if path, ok := p.Args["path"].(string); ok && path != "" {
		fmt.Fprintf(&b, "\n%s", path)
	}
	for _, detail := range ApprovalDetails(p) {
		fmt.Fprintf(&b, "\n%s: %s", detail.Label, detail.Value)
	}
	parts := []string{fmt.Sprintf("/approve %s 仅本次允许", p.ID)}
	if p.Approval.CanRemember() {
		parts = append(parts, fmt.Sprintf("/always %s 始终允许", p.ID))
	}
	parts = append(parts, fmt.Sprintf("/reject %s 拒绝", p.ID))
	fmt.Fprintf(&b, "\n回复 %s", strings.Join(parts, ", "))
	return b.String()
}

func friendlyApprovalReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "dynamic shell execution":
		return "命令包含运行时才能确定的间接执行"
	case "command requires public network access":
		return "命令需要访问公共网络"
	default:
		return reason
	}
}

func friendlyStructureList(structures []string) string {
	values := append([]string(nil), structures...)
	labels := map[string]string{
		"command_substitution": "命令替换", "process_substitution": "进程替换",
		"dynamic_command": "动态命令名", "eval": "eval 间接执行", "source": "加载脚本",
		"shell_command_string": "Shell -c 命令字符串", "shell_heredoc_code": "Shell heredoc 代码",
		"unparseable": "无法可靠解析",
	}
	for i, value := range values {
		if label := labels[value]; label != "" {
			values[i] = label
		}
	}
	return strings.Join(values, "、")
}

func friendlyCapabilityList(value string) string {
	labels := map[string]string{
		"net.outbound": "出站网络", "fs.write.cache": "写入缓存",
		"fs.write.path": "写入外部目录", "process.host": "在宿主机执行",
	}
	parts := strings.Split(value, ",")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if label := labels[part]; label != "" {
			part = label
		}
		parts[i] = part
	}
	return strings.Join(parts, "、")
}

// QuestionText is the canonical plain-text rendering of a pending question,
// with the /answer slash command as the reply path.
func QuestionText(p controlruntime.QuestionView) string {
	var b strings.Builder
	b.WriteString("NekoCode 提问:")
	for _, q := range p.Questions {
		fmt.Fprintf(&b, "\n- %s", q.Question)
		for _, opt := range q.Options {
			fmt.Fprintf(&b, "\n    · %s", opt.Label)
		}
	}
	b.WriteString("\n回复 /answer <回答内容> 作答")
	return b.String()
}

// TruncateRunes shortens s to at most n runes, appending an ellipsis when
// truncated.
func TruncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// StatusView builds the baseline connector status view (running/stopped)
// so channels only fill in configuration, metadata, and devices.
func StatusView(name string, running bool) controlruntime.ConnectorView {
	status := "stopped"
	if running {
		status = "running"
	}
	return controlruntime.ConnectorView{
		Name:        name,
		Registered:  true,
		Initialized: true,
		Running:     running,
		Status:      status,
		Metadata:    make(map[string]any),
	}
}
