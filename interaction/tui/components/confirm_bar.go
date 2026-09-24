// confirm_bar.go — 确认弹窗栏（上下键选择 + Enter 确认）。
package components

import (
	"fmt"
	"strings"

	"nekocode/interaction/tui/styles"
	controlruntime "nekocode/runtime"

	"charm.land/lipgloss/v2"
)

type ConfirmBar struct {
	req      *controlruntime.ConfirmRequest
	action   *actionConfirmation
	sty      *styles.Styles
	selected int
	respond  func(ok bool, remember bool)
}

type actionConfirmation struct {
	title       string
	description string
	acceptLabel string
}

func NewConfirmBar(sty *styles.Styles) *ConfirmBar {
	return &ConfirmBar{sty: sty, selected: 0}
}

func (c *ConfirmBar) SetRequest(req *controlruntime.ConfirmRequest, respond func(ok bool, remember bool)) {
	c.req = req
	c.action = nil
	c.selected = 0
	c.respond = respond
}

// SetDestructive asks for confirmation of a local destructive UI action.
// Unlike permission requests, it deliberately offers only delete and cancel.
// Options render horizontally with cancel first, so index 0 is the safe choice.
func (c *ConfirmBar) SetDestructive(title, description, acceptLabel string, respond func(ok bool)) {
	c.req = nil
	c.action = &actionConfirmation{title: title, description: description, acceptLabel: acceptLabel}
	c.selected = 0 // Default to the safe choice: cancel.
	if respond == nil {
		c.respond = nil
	} else {
		c.respond = func(ok bool, _ bool) { respond(ok) }
	}
}

func (c *ConfirmBar) Clear() {
	c.req = nil
	c.action = nil
	c.respond = nil
}

func (c *ConfirmBar) Selected() int {
	if c.req == nil && c.action == nil {
		return 0
	}
	return c.selected
}

func (c *ConfirmBar) IsDestructive() bool { return c.action != nil }

func (c *ConfirmBar) Move(delta int) {
	if c.req == nil && c.action == nil {
		return
	}
	n := len(c.options())
	if n == 0 {
		return
	}
	c.selected = (c.selected + delta + n) % n
}

func (c *ConfirmBar) Submit() {
	if c.req == nil && c.action == nil {
		return
	}
	opts := c.options()
	if c.selected >= len(opts) {
		c.selected = 0
	}
	opts[c.selected].Action(c)
}

func (c *ConfirmBar) Respond(ok bool, remember bool) {
	if c.respond != nil {
		c.respond(ok, remember)
	}
	c.req = nil
	c.action = nil
	c.respond = nil
}

// CanRemember reports whether the user can persist the decision. The legacy
// capability model only persists "project"-scope grants; the new rule engine
// persists an allow Rule for any ask except CapProcessHost, which is
// intentionally non-persistent (every host-execution must prompt).
func (c *ConfirmBar) CanRemember() bool {
	if c.req == nil || c.action != nil {
		return false
	}
	return c.req.Approval.CanRemember()
}

// options builds one canonical decision set. When the request includes
// predicted sandbox capabilities, the same decision atomically covers the
// command and the capabilities shown in the card.
func (c *ConfirmBar) options() []confirmOption {
	if c.action != nil {
		acceptLabel := c.action.acceptLabel
		if acceptLabel == "" {
			acceptLabel = "确认"
		}
		// Cancel first: the safe default sits at index 0 and the destructive
		// accept action renders last, in the danger palette.
		return []confirmOption{
			{Label: "取消", Action: func(c *ConfirmBar) { c.Respond(false, false) }},
			{Label: acceptLabel, Action: func(c *ConfirmBar) { c.Respond(true, false) }},
		}
	}
	if c.req == nil {
		return nil
	}
	opts := []confirmOption{
		{Label: "仅本次允许", Action: func(c *ConfirmBar) { c.Respond(true, false) }},
	}
	if c.CanRemember() {
		opts = append(opts, confirmOption{Label: "始终允许", Action: func(c *ConfirmBar) { c.Respond(true, true) }})
	}
	opts = append(opts, confirmOption{Label: "拒绝", Action: func(c *ConfirmBar) { c.Respond(false, false) }})
	return opts
}

type confirmOption struct {
	Label  string
	Action func(*ConfirmBar)
}

func confirmMaxLines(termHeight int) int {
	n := termHeight / 3
	if n < 6 {
		n = 6
	}
	return n
}

func (c *ConfirmBar) Height(width, termHeight int) int {
	if c.req == nil && c.action == nil {
		return 0
	}
	contentW := max(40, width-6)
	maxLines := confirmMaxLines(termHeight)
	n := len(c.descLines(contentW))
	if n > maxLines {
		n = maxLines + 1
	}
	if c.action != nil {
		// Destructive layout is compact: options share one horizontal line
		// and there is no separate risk-tag row.
		// title(1) + desc(n) + opts(1) + navSep(1) + navHint(1) + bottom(1)
		return n + 5
	}
	opts := len(c.options())
	// title(1) + desc(n) + sep(1) + levelTag(1) + opts + navSep(1) + navHint(1) + bottom(1)
	return n + opts + 5
}

func (c *ConfirmBar) View(width, termHeight int) string {
	if c.req == nil && c.action == nil {
		return ""
	}
	barW := max(40, width-4)
	contentW := max(40, width-6)
	maxLines := confirmMaxLines(termHeight)

	titleText := c.titleText()
	// Destructive confirmations use a red title so the danger reads at a glance.
	titleStyle := lipgloss.Style(c.sty.Primary.Bold(true))
	if c.action != nil {
		titleStyle = c.sty.Red.Bold(true)
	}
	title := titleStyle.Render("  " + titleText)
	prefix := "┌─  " + titleText + " "
	rightLen := max(0, barW-lipgloss.Width(prefix)-1)
	rightDash := c.sty.Border.Render(strings.Repeat(styles.Horizontal, rightLen) + "┐")
	titleBar := c.sty.Border.Render("┌─") + title + " " + rightDash

	desc := c.formatDesc()
	rawLines := wrapText("  "+desc, contentW)
	var descLines []string
	for i, line := range rawLines {
		if i == 0 {
			descLines = append(descLines, line)
		} else {
			descLines = append(descLines, "  "+line)
		}
	}
	truncated := len(descLines) > maxLines
	if truncated {
		descLines = descLines[:maxLines]
	}

	opts := c.options()

	sep := c.sty.Border.Render("├" + strings.Repeat(styles.Horizontal, barW-2) + "┤")
	bottomBorder := c.sty.Border.Render("└" + strings.Repeat(styles.Horizontal, barW-2) + "┘")
	// Light separator between options and the nav hint so the hint reads as
	// a footer, not another option.
	navSep := c.sty.Border.Render("├" + strings.Repeat(styles.Horizontal, barW-2) + "┤")

	padTo := func(line string) string {
		return line + strings.Repeat(" ", max(0, barW-lipgloss.Width(line)))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", titleBar)
	for _, line := range descLines {
		fmt.Fprintf(&b, "%s\n", padTo(c.sty.Base.Render(line)))
	}
	if truncated {
		fmt.Fprintf(&b, "%s\n", padTo(c.sty.Muted.Render("  ... (truncated)")))
	}
	if c.action != nil {
		// Destructive confirmations render compactly: the options share one
		// horizontal row right below the description, with no risk-tag row
		// or separator (the red title already signals the danger).
		fmt.Fprintf(&b, "%s\n", padTo(c.horizontalOptions(opts)))
	} else {
		fmt.Fprintf(&b, "%s\n", sep)
		// Risk level tag on its own line above the options.
		fmt.Fprintf(&b, "%s\n", padTo("  "+c.levelText()))
		// Options stacked vertically; the selected one is highlighted.
		for i, opt := range opts {
			var row string
			if i == c.selected {
				row = lipgloss.Style(c.sty.Primary).Bold(true).
					Background(lipgloss.Color(styles.BtnYesBg)).
					Padding(0, 2).
					Render("▸ " + opt.Label)
				row = "  " + row
			} else {
				row = c.sty.Muted.Render("    " + opt.Label)
			}
			fmt.Fprintf(&b, "%s\n", padTo(row))
		}
	}
	// Nav hint footer, visually separated from the options.
	fmt.Fprintf(&b, "%s\n", navSep)
	navHint := "  ↑↓选择  Enter确认  Esc拒绝"
	if c.action != nil {
		navHint = "  ↑↓选择  Enter确认  Esc取消"
	}
	fmt.Fprintf(&b, "%s\n", padTo(c.sty.Muted.Render(navHint)))
	b.WriteString(bottomBorder)

	return b.String()
}

// horizontalOptions renders the options inline on a single row. The last
// option is the destructive accept action and keeps the danger palette
// (dark-red background when selected, red text when idle).
func (c *ConfirmBar) horizontalOptions(opts []confirmOption) string {
	parts := make([]string, 0, len(opts))
	for i, opt := range opts {
		danger := i == len(opts)-1
		switch {
		case i == c.selected:
			bg := styles.BtnYesBg
			fg := lipgloss.Style(c.sty.Primary)
			if danger {
				bg = styles.BtnNoBg
				fg = c.sty.Red
			}
			parts = append(parts, fg.Bold(true).
				Background(lipgloss.Color(bg)).
				Padding(0, 1).
				Render("▸ "+opt.Label))
		case danger:
			parts = append(parts, c.sty.Red.Render(opt.Label))
		default:
			parts = append(parts, c.sty.Muted.Render(opt.Label))
		}
	}
	return " " + strings.Join(parts, "  ")
}

func (c *ConfirmBar) titleText() string {
	if c.action != nil {
		return c.action.title
	}
	if c.isExecutionApproval() {
		return "执行确认"
	}
	if c.isPermissionConfirm() {
		return "权限确认"
	}
	if c.req.Kind == controlruntime.ConfirmKindInstall {
		return "安装确认"
	}
	return "Confirm"
}

func (c *ConfirmBar) levelText() string {
	if c.isPermissionConfirm() {
		if c.req.Approval != nil && c.req.Approval.Scope == controlruntime.ApprovalScopeOnce {
			return c.sty.Yellow.Render("临时授权")
		}
		return c.sty.Yellow.Render("可记住")
	}
	if c.req.Kind == controlruntime.ConfirmKindInstall {
		return c.sty.Yellow.Render("插件")
	}
	return c.sty.Yellow.Render("确认")
}

func (c *ConfirmBar) descLines(maxW int) []string {
	desc := c.formatDesc()
	if desc == "" {
		return nil
	}
	return wrapText("  "+desc, maxW)
}

func (c *ConfirmBar) formatDesc() string {
	if c.action != nil {
		return c.action.description
	}
	if c.isPermissionConfirm() {
		return c.formatPermissionDesc()
	}
	switch c.req.ToolName {
	case "shell":
		if cmd, ok := c.req.Args["command"].(string); ok && cmd != "" {
			return formatCommandPreview(cmd, 600)
		}
	case "write":
		if p, ok := c.req.Args["path"].(string); ok && p != "" {
			return "write " + p
		}
	case "edit":
		if p, ok := c.req.Args["path"].(string); ok && p != "" {
			return "edit " + p
		}
	case "/plugin install":
		if summary, ok := c.req.Args["summary"].(string); ok && summary != "" {
			return summary
		}
		return "Install plugin from " + fmt.Sprint(c.req.Args["source"])
	}
	if p, ok := c.req.Args["path"].(string); ok && p != "" {
		return c.req.ToolName + " " + p
	}
	return c.req.ToolName
}

func (c *ConfirmBar) isPermissionConfirm() bool {
	return c.req != nil && c.req.Approval != nil
}

func (c *ConfirmBar) isCombinedApproval() bool {
	return c.req != nil && c.req.Approval != nil && c.req.Approval.Combined
}

func (c *ConfirmBar) isExecutionApproval() bool {
	return c.isCombinedApproval() ||
		(c.req != nil && c.req.Approval != nil && len(c.req.Approval.Structures) > 0)
}

func (c *ConfirmBar) formatPermissionDesc() string {
	var lines []string
	lines = append(lines, permissionSummary(c.req))
	if c.isExecutionApproval() {
		if command, ok := c.req.Args["command"].(string); ok && command != "" {
			lines = append(lines, "命令: "+formatCommandPreview(command, 600))
		}
	}
	approval := c.req.Approval
	if approval == nil {
		return strings.Join(lines, "\n")
	}
	approvalReason := approval.Risk
	if approvalReason != "" {
		lines = append(lines, "风险: "+friendlyPermissionReason(approvalReason))
	}
	if len(approval.Structures) > 0 {
		lines = append(lines, "结构: "+friendlyShellStructures(approval.Structures))
	}
	if reason := approval.Reason; reason != "" {
		if reason != approvalReason {
			lines = append(lines, "原因: "+friendlyPermissionReason(reason))
		}
	}
	if path, ok := c.req.Args["path"].(string); ok && path != "" {
		lines = append(lines, "路径: "+path)
	}
	reqPath, _ := c.req.Args["requested_path"].(string)
	if reqPath != "" && reqPath != c.req.Args["path"] {
		lines = append(lines, "请求: "+reqPath)
	}
	if len(approval.Capabilities) > 0 {
		lines = append(lines, "权限: "+friendlyCapabilities(strings.Join(approval.Capabilities, ", ")))
	}
	if approval.Scope != "" {
		lines = append(lines, "范围: "+friendlyPermissionScope(string(approval.Scope)))
	}
	if approval.Workspace != "" {
		lines = append(lines, "工作区: "+approval.Workspace)
	}
	if len(approval.WritePaths) > 0 {
		lines = append(lines, "可写目录: "+strings.Join(approval.WritePaths, "、"))
	}
	// Footer note for jev-judged approvals: the dialog must make it obvious
	// that the machine judge (not a rule) sent this call to the user.
	if strings.HasPrefix(approval.Risk, "jev:") || strings.HasPrefix(approval.Reason, "jev:") {
		lines = append(lines, "※ 本命令经过 Jev 判定，需要授权确认")
	}
	return strings.Join(lines, "\n")
}

func permissionSummary(req *controlruntime.ConfirmRequest) string {
	if req == nil {
		return "需要确认权限"
	}
	if req.Approval != nil && req.Approval.Combined {
		if len(req.Approval.Capabilities) > 0 {
			return "将执行命令并开放所列权限"
		}
		return "命令包含需要确认的动态执行结构"
	}
	if req.Approval != nil && len(req.Approval.Structures) > 0 {
		return "命令包含需要确认的动态执行结构"
	}
	if req.Approval == nil {
		return "需要确认权限"
	}
	switch req.Approval.Scope {
	case controlruntime.ApprovalScopeOnce:
		return "需要临时授权"
	case controlruntime.ApprovalScopeProject:
		return "需要项目级授权"
	}
	return "需要确认权限"
}

func friendlyPermissionReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "command contains dynamic shell syntax that cannot be safely persisted":
		return "命令包含动态 Shell 语法，无法安全记住为固定规则。"
	case "dynamic shell execution":
		return "命令包含运行时才能确定的间接执行。"
	case "command requires public network access":
		return "命令需要访问公共网络。"
	case "jev: judged dangerous":
		return "Jev 判定该命令存在风险。"
	case "jev: URL judged risky by the risk judge":
		return "Jev 判定该 URL 存在风险。"
	}
	return reason
}

func friendlyShellStructures(structures []string) string {
	labels := make([]string, 0, len(structures))
	for _, structure := range structures {
		switch structure {
		case "command_substitution":
			labels = append(labels, "命令替换")
		case "process_substitution":
			labels = append(labels, "进程替换")
		case "dynamic_command":
			labels = append(labels, "动态命令名")
		case "eval":
			labels = append(labels, "eval")
		case "source":
			labels = append(labels, "source")
		case "shell_command_string":
			labels = append(labels, "Shell -c")
		case "shell_heredoc_code":
			labels = append(labels, "heredoc 内联代码")
		case "unparseable":
			labels = append(labels, "无法解析的 Shell 语法")
		default:
			labels = append(labels, structure)
		}
	}
	return strings.Join(labels, "、")
}

func friendlyCapabilities(caps string) string {
	parts := strings.Split(caps, ",")
	var out []string
	for _, part := range parts {
		cap := strings.TrimSpace(part)
		if cap == "" {
			continue
		}
		switch cap {
		case "net.outbound":
			out = append(out, "出站网络")
		case "fs.write.cache":
			out = append(out, "写入缓存")
		case "fs.write.path":
			out = append(out, "写入外部目录")
		case "process.host":
			out = append(out, "主机执行")
		default:
			out = append(out, cap)
		}
	}
	return strings.Join(out, "、")
}

func friendlyPermissionScope(scope string) string {
	switch strings.TrimSpace(scope) {
	case "once":
		return "仅本次，不会记住"
	case "project":
		return "当前工作区，可选择记住"
	case "":
		return ""
	}
	return scope
}

func wrapText(text string, maxW int) []string {
	if maxW <= 0 {
		return []string{text}
	}
	var lines []string
	remaining := []rune(text)
	for len(remaining) > 0 {
		displayW := lipgloss.Width(string(remaining))
		if displayW <= maxW && !strings.ContainsRune(string(remaining), '\n') {
			lines = append(lines, strings.TrimRight(string(remaining), "\n"))
			break
		}
		cut := 0
		w := 0
		lastSpace := -1
		for i, r := range remaining {
			if r == '\n' {
				lines = append(lines, string(remaining[:i]))
				remaining = remaining[i+1:]
				cut = -1
				break
			}
			rw := lipgloss.Width(string(r))
			if w+rw > maxW {
				break
			}
			w += rw
			cut = i + 1
			if r == ' ' {
				lastSpace = i
			}
		}
		if cut < 0 {
			continue
		}
		if lastSpace > 0 && lastSpace < cut {
			cut = lastSpace
		}
		if cut == 0 {
			cut = 1
		}
		lines = append(lines, strings.TrimRight(string(remaining[:cut]), " "))
		remaining = remaining[cut:]
		if len(remaining) > 0 && remaining[0] == ' ' {
			remaining = remaining[1:]
		}
	}
	return lines
}
