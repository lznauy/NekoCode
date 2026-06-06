// processing_render.go — Render 编排器 + 6 个 section 渲染方法。
package processing

import (
	"fmt"
	"strings"

	"nekocode/tui/components/block"
	"nekocode/tui/styles"

	"charm.land/lipgloss/v2"
)

func (p *ProcessingItem) Render(width int) string {
	if p.cachedRenderW == width && p.cachedRender != "" {
		return p.cachedRender
	}
	cw := width - 4
	contentW := cw - 4

	var sections []string
	sections = append(sections, p.renderSummary())
	if s := p.renderActivitySection(contentW, contentW); s != "" {
		sections = append(sections, s)
	}
	if s := p.renderChangesSection(contentW, contentW); s != "" {
		sections = append(sections, s)
	}
	if s := p.renderTodos(contentW, contentW); s != "" {
		sections = append(sections, s)
	}
	if s := p.renderOutputSection(contentW, contentW); s != "" {
		sections = append(sections, s)
	}
	if s := p.renderReasoningSection(contentW, contentW); s != "" {
		sections = append(sections, s)
	}
	sections = append(sections, p.renderHeader())

	body := strings.Join(sections, "\n")
	card := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color(styles.Primary)).
		PaddingLeft(0).PaddingRight(0).Width(cw).MaxWidth(cw).Render(body)

	p.cachedRender = card
	p.cachedRenderW = width
	p.cachedHeight = strings.Count(card, "\n") + 1
	return card
}

func (p *ProcessingItem) renderHeader() string {
	s := p.spinnerView
	if s == "" {
		s = "..."
	}
	l := p.statusText
	if l == "" {
		l = "Thinking"
	}
	sk := ""
	if p.skill != "" {
		sk = " " + p.sty.Yellow.Render("skill:"+p.skill)
	}
	tp := ""
	if p.tokenPrompt > 0 || p.tokenCompl > 0 {
		tp = " " + p.sty.Subtle.Render("↑"+styles.FmtTokens(p.tokenPrompt)+" ↓"+styles.FmtTokens(p.tokenCompl))
	}
	if p.compactCount > 0 {
		tp += " " + p.sty.Subtle.Render(fmt.Sprintf("🧹%d", p.compactCount))
	}
	return "\n" + p.sty.Teal.Render(s) + " " + p.sty.Subtle.Render(l) + sk + tp
}

// -- summary ---------------------------------------------------------------

func (p *ProcessingItem) renderSummary() string {
	actN, chgN := 0, 0
	for _, b := range p.blocks {
		if b.Type != block.BlockTool {
			continue
		}
		if isActivityTool(b.ToolName) {
			actN++
		} else {
			chgN++
		}
	}
	var parts []string
	if p.skill != "" {
		parts = append(parts, "skill: "+p.skill)
	}
	if actN > 0 {
		parts = append(parts, fmt.Sprintf("%d tools", actN))
	}
	if chgN > 0 {
		parts = append(parts, fmt.Sprintf("%d changes", chgN))
	}
	if len(parts) == 0 {
		return p.sty.Subtle.Render("(=^.^=)")
	}
	return p.sty.Subtle.Render("(=^.^=) · " + strings.Join(parts, " · "))
}

// -- activity section -------------------------------------------------------

func (p *ProcessingItem) renderActivitySection(contentW, sepW int) string {
	var items []block.ContentBlock
	for _, b := range p.blocks {
		if b.Type == block.BlockTool && isActivityTool(b.ToolName) {
			items = append(items, b)
		}
	}
	if len(items) == 0 {
		return ""
	}
	total := len(items)
	if p.cachedActivityW == sepW && p.cachedActivityN == total && p.cachedActivity != "" {
		return p.cachedActivity
	}

	start := 0
	if total > maxActivity {
		start = total - maxActivity
	}
	items = items[start:]

	var sb strings.Builder
	sb.WriteString("\n")
	sep := p.sty.Subtle.Render("── activity " + strings.Repeat("─", sepW-lipgloss.Width("── activity ")))
	sb.WriteString(sep)
	sb.WriteString("\n")
	for _, b := range items {
		fmt.Fprintf(&sb, "  ◉ %s", b.ToolName)
		if b.ToolArgs != "" {
			fmt.Fprintf(&sb, " %s", b.ToolArgs)
		}
		if !b.Done && b.Content == "" {
			fmt.Fprintf(&sb, " %s", p.sty.Subtle.Render("…"))
		}
		sb.WriteString("\n")
	}

	p.cachedActivity = sb.String()
	p.cachedActivityW = sepW
	p.cachedActivityN = total
	return p.cachedActivity
}

// -- changes section --------------------------------------------------------

func (p *ProcessingItem) renderChangesSection(contentW, sepW int) string {
	var items []block.ContentBlock
	for _, b := range p.blocks {
		if b.Type == block.BlockTool && !isActivityTool(b.ToolName) {
			items = append(items, b)
		}
	}
	if len(items) == 0 {
		return ""
	}
	total := len(items)
	if p.cachedChangesW == sepW && p.cachedChangesN == total && p.cachedChanges != "" {
		return p.cachedChanges
	}

	var sb strings.Builder
	sb.WriteString("\n")
	sep := p.sty.Subtle.Render("── changes " + strings.Repeat("─", sepW-lipgloss.Width("── changes ")))
	sb.WriteString(sep)
	sb.WriteString("\n")
	sb.WriteString(block.RenderTools(items, contentW, p.sty))

	p.cachedChanges = sb.String()
	p.cachedChangesW = sepW
	p.cachedChangesN = total
	return p.cachedChanges
}

// -- tasks section ----------------------------------------------------------

func (p *ProcessingItem) renderTodos(contentW, sepW int) string {
	if p.todos == "" {
		return ""
	}
	if p.cachedTodosW < 0 || sepW != p.cachedTodosW {
		green := lipgloss.NewStyle().Foreground(lipgloss.Color(styles.DiffGreen))
		var sb strings.Builder
		sb.WriteString("\n")
		sep := p.sty.Yellow.Render("── tasks " + strings.Repeat("─", sepW-lipgloss.Width("── tasks ")))
		sb.WriteString(sep)
		sb.WriteString("\n\n")
		for _, line := range strings.Split(p.todos, "\n") {
			switch {
			case strings.HasPrefix(line, "✓ All"):
				sb.WriteString(green.Render(line))
			case strings.HasPrefix(line, "Tasks "):
				sb.WriteString(p.sty.Subtle.Render(line))
			case strings.HasPrefix(line, "·"):
				sb.WriteString(p.sty.Subtle.Render(line))
			case strings.HasPrefix(line, "▸"):
				sb.WriteString(p.sty.Teal.Render(line))
			case strings.HasPrefix(line, "✓"):
				sb.WriteString(green.Render(line))
			default:
				sb.WriteString(line)
			}
			sb.WriteString("\n")
		}
		p.cachedTodos = sb.String()
		p.cachedTodosW = sepW
	}
	return p.cachedTodos
}

// -- output section ---------------------------------------------------------

func (p *ProcessingItem) renderOutputSection(contentW, sepW int) string {
	text := strings.TrimSpace(p.OutputText())
	if text == "" {
		return ""
	}
	content := RenderFixed(WrapPlain(text, contentW), outputLines, true, p.sty.Subtle)
	if content == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n")
	sep := p.sty.Teal.Render("── output " + strings.Repeat("─", sepW-lipgloss.Width("── output ")))
	sb.WriteString(sep)
	sb.WriteString("\n\n")
	sb.WriteString(content)
	return sb.String()
}

// -- reasoning section ------------------------------------------------------

func (p *ProcessingItem) renderReasoningSection(contentW, sepW int) string {
	if p.ReasoningText() == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n")
	sep := p.sty.Blue.Render("── reasoning " + strings.Repeat("─", sepW-lipgloss.Width("── reasoning ")))
	sb.WriteString(sep)
	sb.WriteString("\n\n")
	sb.WriteString(RenderFixed(WrapPlain(p.ReasoningText(), contentW), reasonLines, false, p.sty.Muted))
	return sb.String()
}
