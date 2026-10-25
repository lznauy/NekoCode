// processing_render.go — Render 编排器 + 6 个 section 渲染方法。
package processing

import (
	"fmt"
	"strings"

	"nekocode/interaction/tui/components/block"
	"nekocode/interaction/tui/styles"
	"nekocode/util/text"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (p *ProcessingItem) Render(width int) string { return p.render(width, 0) }

// RenderWithin uses the same sections, frame and caches as Render, compressing
// section bodies only when the available viewport cannot fit the full card.
func (p *ProcessingItem) RenderWithin(width, height int) string {
	if height <= 0 {
		return ""
	}
	return p.render(width, height)
}

func (p *ProcessingItem) render(width, maxHeight int) string {
	if maxHeight > 0 && p.cachedConstrained != "" && p.cachedConstrainedW == width && p.cachedConstrainedH == maxHeight {
		return p.cachedConstrained
	}
	contentW := max(1, styles.MessageWidth(width)-4)
	var sections []string
	if p.cachedRenderW != width || p.cachedRender == "" {
		sections = p.renderSections(contentW)
		p.cachedRender = renderCard(width, strings.Join(sections, "\n\n"))
		p.cachedRenderW = width
		p.cachedHeight = strings.Count(p.cachedRender, "\n") + 1
	}
	if maxHeight <= 0 || p.cachedHeight <= maxHeight {
		return p.cachedRender
	}
	if maxHeight < 4 {
		return ansi.Truncate(p.renderHeader(max(1, width)), max(1, width), "…")
	}
	if sections == nil {
		sections = p.renderSections(contentW)
	}
	middle := sections[1 : len(sections)-1]
	budget := maxHeight - 4 // frame, summary and status
	if len(middle) > budget {
		middle = middle[len(middle)-budget:]
	}
	lines := []string{sections[0]}
	for i, section := range middle {
		rows := budget / (len(middle) - i)
		budget -= rows
		var body []string
		for _, line := range strings.Split(section, "\n") {
			if strings.TrimSpace(ansi.Strip(line)) != "" {
				body = append(body, line)
			}
		}
		if len(body) == 0 {
			continue
		}
		if rows == 1 && len(body) > 1 {
			label := strings.TrimRight(ansi.Strip(body[0]), "─ ")
			lines = append(lines, ansi.Truncate(label+" · "+strings.TrimSpace(ansi.Strip(body[len(body)-1])), contentW, "…"))
			continue
		}
		lines = append(lines, body[0])
		start := max(1, len(body)-rows+1)
		lines = append(lines, body[start:]...)
	}
	lines = append(lines, sections[len(sections)-1])
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, contentW, "…")
	}
	card := renderCard(width, strings.Join(lines, "\n"))
	p.cachedConstrained, p.cachedConstrainedW, p.cachedConstrainedH = card, width, maxHeight
	return card
}

func renderCard(width int, body string) string {
	cw := styles.MessageWidth(width)
	return lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color(styles.Primary)).
		Padding(0, 1).Width(cw).MaxWidth(cw).Render(body)
}

func (p *ProcessingItem) renderSections(contentW int) []string {
	heading := strings.TrimRight(p.renderSummary(), "\n")
	if p.subagentSummary != "" && contentW < 40 {
		heading = p.sty.Subtle.Render(p.subagentSummary)
	}
	sections := []string{ansi.Truncate(heading, contentW, "…")}
	for _, render := range [...]func(int) string{
		p.renderActivitySection, p.renderChangesSection, p.renderTodos,
		p.renderOutputSection, p.renderThinkingSection, p.renderCompactions,
	} {
		if section := strings.TrimRight(render(contentW), "\n"); section != "" {
			sections = append(sections, section)
		}
	}
	return append(sections, p.renderHeader(contentW))
}

func (p *ProcessingItem) renderHeader(width int) string {
	s := p.spinnerView
	if s == "" {
		s = "..."
	}
	l := p.statusText
	if l == "" {
		l = "Thinking"
	}
	if len(p.compactions) > 0 && p.compactions[len(p.compactions)-1].Running() {
		l = "Compacting context..."
	}

	var meta []string
	if p.skill != "" {
		meta = append(meta, p.sty.Yellow.Render("skill:"+p.skill))
	}
	if p.tokenPrompt > 0 || p.tokenCompl > 0 {
		meta = append(meta, p.sty.Muted.Render("↑"+text.FormatTokens(p.tokenPrompt)+" ↓"+text.FormatTokens(p.tokenCompl)))
	}
	if p.compactCount > 0 {
		meta = append(meta, p.sty.Muted.Render(fmt.Sprintf("🧹%d", p.compactCount)))
	}

	spinner := p.sty.Teal.Render(s)
	const gap = " "
	tail := strings.Join(meta, gap)
	const indent = ""
	statusW := width - lipgloss.Width(indent) - lipgloss.Width(spinner) - lipgloss.Width(gap)
	if tail != "" {
		statusW -= lipgloss.Width(tail) + lipgloss.Width(gap)
	}
	if statusW < 12 && tail != "" {
		tail = ""
		statusW = width - lipgloss.Width(indent) - lipgloss.Width(spinner) - lipgloss.Width(gap)
	}

	line := indent + spinner + gap + p.sty.Base.Render(truncatePlain(l, max(1, statusW)))
	if tail != "" {
		line += gap + tail
	}
	return ansi.Truncate(line, max(1, width), "…")
}

func truncatePlain(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	var out strings.Builder
	for _, r := range s {
		next := out.String() + string(r)
		if lipgloss.Width(next)+1 > width {
			break
		}
		out.WriteRune(r)
	}
	return out.String() + "…"
}

func sectionRule(label string, width int) string {
	return truncatePlain(label+strings.Repeat("─", max(0, width-lipgloss.Width(label))), width)
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
	if p.subagentSummary != "" {
		parts = append(parts, p.subagentSummary)
	}
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
		if s.ColorIdx >= 0 {
			c := lipgloss.Color(styles.SubColors[s.ColorIdx%len(styles.SubColors)])
			headerBuilder.WriteByte(' ')
			headerBuilder.WriteString(lipgloss.NewStyle().Foreground(c).Render(styles.SubBullet))
		}
	}
	header := headerBuilder.String()
	if len(parts) == 0 {
		return header + "\n"
	}
	return header + p.sty.Subtle.Render(" · "+strings.Join(parts, " · ")) + "\n"
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
	sep := p.sty.Primary.Render(sectionRule("── activity ", sepW))
	sb.WriteString(sep)
	sb.WriteString("\n")
	for _, b := range items {
		// Choose bullet based on sub-agent color
		bullet, bulletStyle := styles.BulletForBlock(b.SubID, b.SubColor, p.sty.Teal)
		fmt.Fprintf(&sb, "  %s %s", bulletStyle.Render(bullet), p.sty.Base.Bold(true).Render(b.ToolName))
		if b.ToolArgs != "" {
			fmt.Fprintf(&sb, " %s", p.sty.Muted.Render(b.ToolArgs))
		}
		sb.WriteString("\n")
		if b.JevNote != "" {
			fmt.Fprintf(&sb, "    %s\n", p.sty.Subtle.MaxWidth(max(10, sepW-4)).Render(b.JevNote))
		}
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
	sep := p.sty.Primary.Render(sectionRule("── changes ", w))
	sb.WriteString(sep)
	sb.WriteString("\n")
	sb.WriteString(block.RenderTools(items, w, p.sty))
	sb.WriteString("\n")

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

		sep := p.sty.Yellow.Render(sectionRule("── tasks ", w))
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
	if p.cachedOutputW == w {
		return p.cachedOutput
	}
	p.cachedOutputW = w
	p.cachedOutput = ""
	text := strings.TrimSpace(p.OutputText())
	if text == "" {
		return ""
	}
	content := RenderFixed(wrapTail(text, w, outputLines), outputLines, true, p.sty.Base)
	if content == "" {
		return ""
	}
	var sb strings.Builder

	sep := p.sty.Primary.Render(sectionRule("── output ", w))
	sb.WriteString(sep)
	sb.WriteString("\n")
	sb.WriteString(content)

	p.cachedOutput = sb.String()
	return p.cachedOutput
}

// -- thinking section ------------------------------------------------------

func (p *ProcessingItem) renderThinkingSection(w int) string {
	if p.cachedThinkingW == w {
		return p.cachedThinking
	}
	p.cachedThinkingW = w
	p.cachedThinking = ""
	if p.ThinkingText() == "" {
		return ""
	}
	var sb strings.Builder

	sep := p.sty.Muted.Render(sectionRule("── thinking ", w))
	sb.WriteString(sep)
	sb.WriteString("\n")
	sb.WriteString(RenderFixed(wrapTail(p.ThinkingText(), w, thinkLines), thinkLines, false, p.sty.Muted))

	p.cachedThinking = sb.String()
	return p.cachedThinking
}
