// processing_render.go — Render 编排器 + 5 个 section 渲染方法。
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
	cw := p.contentWidth
	if cw <= 0 {
		cw = width - 4
	}
	contentW := cw - 4

	var sections []string
	sections = append(sections, p.renderHeader())
	if s := p.renderTodos(cw); s != "" {
		sections = append(sections, s)
	}
	if s := p.renderToolSection(contentW, cw); s != "" {
		sections = append(sections, s)
	}
	if s := p.renderOutputSection(contentW); s != "" {
		sections = append(sections, s)
	}
	if s := p.renderReasoningSection(contentW); s != "" {
		sections = append(sections, s)
	}

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
	return p.sty.Teal.Render(s) + " " + p.sty.Subtle.Render(l) + sk + tp
}

func (p *ProcessingItem) renderTodos(cw int) string {
	if p.todos == "" {
		return ""
	}
	if p.cachedTodosW < 0 || cw != p.cachedTodosW {
		green := lipgloss.NewStyle().Foreground(lipgloss.Color(styles.DiffGreen))
		var sb strings.Builder
		for _, line := range strings.Split(p.todos, "\n") {
			sb.WriteString("\n  ")
			switch {
			case strings.HasPrefix(line, "✓ All"):
				// All-complete summary in green.
				sb.WriteString(green.Render(line))
			case strings.HasPrefix(line, "Tasks "):
				// Header line: dim counter.
				sb.WriteString(p.sty.Subtle.Render(line))
			case strings.HasPrefix(line, "·"):
				// Pending: muted.
				sb.WriteString(p.sty.Subtle.Render(line))
			case strings.HasPrefix(line, "▸"):
				// In progress: teal accent.
				sb.WriteString(p.sty.Teal.Render(line))
			case strings.HasPrefix(line, "✓"):
				// Completed: green.
				sb.WriteString(green.Render(line))
			default:
				sb.WriteString(line)
			}
		}
		p.cachedTodos = sb.String()
		p.cachedTodosW = cw
	}
	return p.cachedTodos
}

func (p *ProcessingItem) renderToolSection(contentW, cw int) string {
	// Fast path: if the tool cache is valid and count hasn't changed, reuse it.
	// invalidateLight preserves cachedToolN; invalidate resets it to 0.
	blockCount := len(p.blocks)
	if blockCount == p.cachedToolN && cw == p.cachedToolW && p.cachedTool != "" {
		return p.cachedTool
	}
	// Rebuild: count tool blocks and render if any exist.
	toolN := 0
	for _, b := range p.blocks {
		if b.Type == block.BlockTool {
			toolN++
		}
	}
	p.cachedTool = ""
	if toolN > 0 {
		p.cachedTool = p.renderToolBlocks(contentW)
	}
	p.cachedToolN = blockCount
	p.cachedToolW = cw
	return p.cachedTool
}

// renderToolBlocks 渲染处理中的工具块列表。
func (p *ProcessingItem) renderToolBlocks(contentW int) string {
	if len(p.blocks) == 0 {
		return ""
	}
	return "\n" + block.RenderTools(p.blocks, contentW, p.sty)
}

func (p *ProcessingItem) renderOutputSection(contentW int) string {
	text := strings.TrimSpace(p.OutputText())
	if text == "" {
		return ""
	}
	content := RenderFixed(WrapPlain(text, contentW), outputLines, true, p.sty.Subtle)
	if content == "" {
		return ""
	}
	var sb strings.Builder
	if p.cachedTool != "" {
		sb.WriteString("\n")
	}
	sep := p.sty.Teal.Render("▍ output " + strings.Repeat("─", contentW-lipgloss.Width("▍ output ")))
	sb.WriteString(sep)
	sb.WriteString("\n\n")
	sb.WriteString(content)
	return sb.String()
}

func (p *ProcessingItem) renderReasoningSection(contentW int) string {
	if p.ReasoningText() == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n")
	sep := p.sty.Blue.Render("▍ reasoning " + strings.Repeat("─", contentW-lipgloss.Width("▍ reasoning ")))
	sb.WriteString(sep)
	sb.WriteString("\n\n")
	sb.WriteString(RenderFixed(WrapPlain(p.ReasoningText(), contentW), reasonLines, false, p.sty.Muted))
	return sb.String()
}
