// confirm_bar.go — 确认弹窗栏（上下键选择 + Enter 确认）。
package components

import (
	"fmt"
	"strings"

	"nekocode/interaction/tui/styles"
	"nekocode/protocol"
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

// CanRemember reports whether every part of this approval can be persisted.
// A once-only command or capability keeps the merged decision once-only.
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
	var opts []confirmOption
	for _, option := range c.req.Approval.DecisionOptions(c.req.ToolName) {
		opts = append(opts, confirmOption{Label: option.Label, Action: func(c *ConfirmBar) { c.Respond(option.Allowed, option.Remember) }})
	}
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

// formatPermissionDesc renders the body of a permission card. Callers reach it
// only through isPermissionConfirm, which guarantees a non-nil approval, so the
// card always has at least one field to show.
func (c *ConfirmBar) formatPermissionDesc() string {
	approval := c.req.Approval
	// The title bar already says what kind of confirmation this is, so the
	// body carries fields only, all in one shape: "标签: 值".
	var lines []string
	approvalReason := approval.Risk
	if approvalReason == "" {
		approvalReason = approval.Reason
	}
	if approval.Rule != "" {
		lines = append(lines, "触发规则: "+approval.Rule)
	} else if label := permissionReasonLabel(approvalReason); label != "" {
		lines = append(lines, "原因: "+label)
	}
	if c.isExecutionApproval() {
		if command, ok := c.req.Args["command"].(string); ok && command != "" {
			lines = append(lines, formatCommandPreview(command, 600))
		}
	}
	if reason := approval.Reason; reason != "" && reason != approvalReason {
		lines = append(lines, "原因: "+friendlyPermissionReason(reason))
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
	if approval.OutboundDomains != nil {
		domains := strings.Join(approval.OutboundDomains, "、")
		if domains == "" {
			domains = "禁止联网"
		}
		lines = append(lines, "网络域名: "+domains)
	}
	if len(approval.WritePaths) > 0 {
		lines = append(lines, "可写目录: "+strings.Join(approval.WritePaths, "、"))
	}
	// Decisions come first: what the user has to decide is the only part that
	// changes the outcome. Reusable grants are grouped behind it as context.
	persistent, session := splitRememberRules(approval.RememberRules)
	if len(persistent) > 0 {
		lines = append(lines, "待确认: "+formatRuleList(persistent))
	}
	if len(session) > 0 {
		lines = append(lines, "本会话: "+formatRuleList(session)+"（会话结束后失效）")
	}
	if len(approval.ApprovedRules) > 0 {
		lines = append(lines, "通过: "+formatRuleList(approval.ApprovedRules))
	}
	if approval.CanRemember() && len(persistent) > 0 && approval.RememberWorkspace != "" {
		lines = append(lines, "长期授权生效项目: "+approval.RememberWorkspace)
	}
	if approval.CanRemember() && len(approval.Capabilities) > 0 {
		lines = append(lines, "同时记住: 目录与网络范围")
	}
	return strings.Join(lines, "\n")
}

// splitRememberRules separates grants that will be written to the project
// permissions file from those only valid for the current session.
func splitRememberRules(rules []protocol.ApprovalRememberRule) (persistent, session []protocol.ApprovalRememberRule) {
	for _, rule := range rules {
		if rule.Session() {
			session = append(session, rule)
			continue
		}
		persistent = append(persistent, rule)
	}
	return persistent, session
}

// maxListedSpecs bounds how many specifiers a summary line spells out. Long
// scripts produce dozens of units, and grouping by tool alone would let one
// tool's list grow without limit: the count is what matters past a few.
const maxListedSpecs = 3

// formatRuleList renders rules compactly: reusable entries of the same tool
// share one pair of parentheses, exact statements collapse to a short summary
// (the full script is already shown as the command preview above), and long
// lists collapse into a count.
func formatRuleList(rules []protocol.ApprovalRememberRule) string {
	if len(rules) == 0 {
		return ""
	}
	var parts []string
	grouped := map[string][]string{}
	var order []string
	omitted := 0
	flush := func() {
		for _, tool := range order {
			specs := grouped[tool]
			if len(specs) > maxListedSpecs {
				omitted += len(specs) - maxListedSpecs
				specs = specs[:maxListedSpecs]
			}
			parts = append(parts, tool+"("+strings.Join(specs, ", ")+")")
		}
		grouped = map[string][]string{}
		order = nil
	}
	exact := 0
	for _, rule := range rules {
		if rule.Literal != "" || rule.Specifier == "" {
			flush()
			exact++
			continue
		}
		if _, ok := grouped[rule.Tool]; !ok {
			order = append(order, rule.Tool)
		}
		grouped[rule.Tool] = append(grouped[rule.Tool], rule.Specifier)
	}
	flush()
	if exact == 1 {
		parts = append(parts, "整段脚本")
	} else if exact > 1 {
		parts = append(parts, fmt.Sprintf("%d 段脚本", exact))
	}
	if len(parts) > maxListedSpecs {
		omitted += len(parts) - maxListedSpecs
		parts = parts[:maxListedSpecs]
	}
	out := strings.Join(parts, "、")
	if omitted > 0 {
		// The count names the whole set, matching the "N 条" wording used on
		// the remember button for the same rules.
		out += fmt.Sprintf(" 等 %d 项", omitted+shownSpecs(parts))
	}
	return out
}

// shownSpecs counts the specifiers actually spelled out in the rendered parts,
// including the ones folded into a per-tool group.
func shownSpecs(parts []string) int {
	count := 0
	for _, part := range parts {
		if strings.HasSuffix(part, "段脚本") {
			count++
			continue
		}
		count += strings.Count(part, ", ") + 1
	}
	return count
}

func friendlyPermissionReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "command contains dynamic shell syntax that cannot be safely persisted":
		return "运行时生成内容，无法记住"
	case "dynamic shell execution":
		return "运行时生成内容"
	case "explicit ask rule requires approval on every call":
		return "你要求每次确认"
	case "command requires public network access":
		return "需要访问公网"
	case "jev: judged dangerous":
		return "风险判定不安全"
	case "jev: judgment failed":
		return "风险判定失败"
	case "jev: URL judged risky by the risk judge":
		return "网址风险判定不安全"
	}
	return reason
}

// permissionReasonLabel renders a reason as a short field value. It returns
// an empty string for reasons that carry no information a user can act on.
func permissionReasonLabel(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	return friendlyPermissionReason(reason)
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
