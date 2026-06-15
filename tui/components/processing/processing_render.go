// processing_render.go — Render 编排器 + 6 个 section 渲染方法。
package processing

import (
	"fmt"
	"strings"

	"nekocode/common"
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
	if s := p.renderActivitySection(contentW); s != "" {
		sections = append(sections, s)
	}
	if s := p.renderChangesSection(contentW); s != "" {
		sections = append(sections, s)
	}
	if s := p.renderTodos(contentW); s != "" {
		sections = append(sections, s)
	}
	if s := p.renderOutputSection(contentW); s != "" {
		sections = append(sections, s)
	}
	if s := p.renderThinkingSection(contentW); s != "" {
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
		tp = " " + p.sty.Muted.Render("↑"+common.FormatTokens(p.tokenPrompt)+" ↓"+common.FormatTokens(p.tokenCompl))
	}
	if p.compactCount > 0 {
		tp += " " + p.sty.Muted.Render(fmt.Sprintf("🧹%d", p.compactCount))
	}
	return p.sty.Teal.Render(s) + " " + p.sty.Base.Render(l) + sk + tp
}

// -- summary ---------------------------------------------------------------

func (p *ProcessingItem) renderSummary() string {
	actN, chgN := 0, 0
	for _, b := range p.blocks {
		if b.Type != block.BlockTool {
			continue
		}
		if !block.IsPersistent(b.ToolName) {
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

	// Cat face with eye color + colored sub-agent bullets
	var headerBuilder strings.Builder
	headerBuilder.WriteString(p.sty.CatBody.Render(styles.CatLeft))
	headerBuilder.WriteString(p.sty.CatEye.Render(styles.CatLEye))
	headerBuilder.WriteString(p.sty.Muted.Render(styles.CatNose))
	headerBuilder.WriteString(p.sty.CatEye.Render(styles.CatREye))
	headerBuilder.WriteString(p.sty.CatBody.Render(styles.CatRight))
	for _, s := range p.subSlots {
		if s.ColorIdx >= 0 && s.ColorIdx < len(styles.SubColors) {
			c := lipgloss.Color(styles.SubColors[s.ColorIdx])
			headerBuilder.WriteByte(' ')
			headerBuilder.WriteString(lipgloss.NewStyle().Foreground(c).Render(styles.SubBullet))
		}
	}
	header := headerBuilder.String()
	if len(parts) == 0 {
		return header + "\n"
	}
	return header + p.sty.Subtle.Render(" · " + strings.Join(parts, " · ")) + "\n"
}

// -- activity section -------------------------------------------------------

func (p *ProcessingItem) renderActivitySection(sepW int) string {
	var items []block.ContentBlock
	for _, b := range p.blocks {
		if b.Type == block.BlockTool && !block.IsPersistent(b.ToolName) {
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
	sep := p.sty.Primary.Render("── activity " + strings.Repeat("─", sepW-lipgloss.Width("── activity ")))
	sb.WriteString(sep)
	sb.WriteString("\n")
	for _, b := range items {
		// Choose bullet based on sub-agent color
		bullet, bulletStyle := styles.BulletForBlock(b.SubID, b.SubColor, p.sty.Teal)
		fmt.Fprintf(&sb, "  %s %s", bulletStyle.Render(bullet), p.sty.Base.Bold(true).Render(b.ToolName))
		if b.ToolArgs != "" {
			fmt.Fprintf(&sb, " %s", p.sty.Muted.Render(b.ToolArgs))
		}
		if !b.Done && b.Content == "" {
			fmt.Fprintf(&sb, " %s", p.sty.Muted.Render("…"))
		}
		sb.WriteString("\n")
	}

	p.cachedActivity = sb.String()
	p.cachedActivityW = sepW
	p.cachedActivityN = total
	return p.cachedActivity
}

// -- changes section --------------------------------------------------------

func (p *ProcessingItem) renderChangesSection(w int) string {
	var items []block.ContentBlock
	for _, b := range p.blocks {
		if b.Type == block.BlockTool && block.IsPersistent(b.ToolName) {
			items = append(items, b)
		}
	}
	if len(items) == 0 {
		return ""
	}
	total := len(items)
	if p.cachedChangesW == w && p.cachedChangesN == total && p.cachedChanges != "" {
		return p.cachedChanges
	}

	var sb strings.Builder
	sep := p.sty.Primary.Render("── changes " + strings.Repeat("─", w-lipgloss.Width("── changes ")))
	sb.WriteString(sep)
	sb.WriteString("\n")
	sb.WriteString(block.RenderTools(items, w, p.sty))

	p.cachedChanges = sb.String()
	p.cachedChangesW = w
	p.cachedChangesN = total
	return p.cachedChanges
}

// -- tasks section ----------------------------------------------------------

func (p *ProcessingItem) renderTodos(w int) string {
	if p.todos == "" {
		return ""
	}
	if p.cachedTodosW < 0 || w != p.cachedTodosW {
		green := lipgloss.NewStyle().Foreground(lipgloss.Color(styles.DiffGreen))
		var sb strings.Builder
		
		sep := p.sty.Yellow.Render("── tasks " + strings.Repeat("─", w-lipgloss.Width("── tasks ")))
		sb.WriteString(sep)
		sb.WriteString("\n")
		for line := range strings.SplitSeq(p.todos, "\n") {
			switch {
			case strings.HasPrefix(line, "✓ All"):
								sb.WriteString("  ")
				sb.WriteString(green.Render(line))
				sb.WriteByte('\n')
			case strings.HasPrefix(line, "Tasks "):
								sb.WriteString("  ")
				sb.WriteString(p.sty.Muted.Render(line))
				sb.WriteByte('\n')
			case strings.HasPrefix(line, "·"):
								sb.WriteString("  ")
				sb.WriteString(p.sty.Muted.Render(line))
				sb.WriteByte('\n')
			case strings.HasPrefix(line, "▸"):
								sb.WriteString("  ")
				sb.WriteString(p.sty.Teal.Render(line))
				sb.WriteByte('\n')
			case strings.HasPrefix(line, "✓"):
								sb.WriteString("  ")
				sb.WriteString(green.Render(line))
				sb.WriteByte('\n')
			default:
								sb.WriteString("  ")
				sb.WriteString(line)
				sb.WriteByte('\n')
			}
			
		}
		p.cachedTodos = sb.String()
		p.cachedTodosW = w
	}
	return p.cachedTodos
}

// -- output section ---------------------------------------------------------

func (p *ProcessingItem) renderOutputSection(w int) string {
	text := strings.TrimSpace(p.OutputText())
	if text == "" {
		return ""
	}
	content := RenderFixed(WrapPlain(text, w), outputLines, true, p.sty.Base)
	if content == "" {
		return ""
	}
	var sb strings.Builder

	sep := p.sty.Primary.Render("── output " + strings.Repeat("─", w-lipgloss.Width("── output ")))
	sb.WriteString(sep)
	sb.WriteString("\n")
	sb.WriteString(content)
	
	return sb.String()
}

// -- thinking section ------------------------------------------------------

func (p *ProcessingItem) renderThinkingSection(w int) string {
	if p.ThinkingText() == "" {
		return ""
	}
	var sb strings.Builder

	sep := p.sty.Muted.Render("── thinking " + strings.Repeat("─", w-lipgloss.Width("── thinking ")))
	sb.WriteString(sep)
	sb.WriteString("\n")
	sb.WriteString(RenderFixed(WrapPlain(p.ThinkingText(), w), thinkLines, false, p.sty.Muted))
	
	return sb.String()
}

