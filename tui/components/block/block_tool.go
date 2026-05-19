// block_tool.go — 工具调用行渲染。
package block

import (
	"fmt"
	"strings"

	"nekocode/tui/styles"

	"charm.land/lipgloss/v2"
)

func renderToolLine(b ContentBlock, width int, sty *styles.Styles) string {
	icon := "◆"
	running := b.Content == "" && (b.ToolName == "edit" || b.ToolName == "task" || b.ToolName == "bash")

	// 摘要：折叠时显示一行关键信息，展开后显示完整内容。
	summary := b.ToolArgs
	if b.Content != "" && b.Collapsed {
		summary = toolSummary(b)
	}

	arrow := ""
	if b.Content != "" || running {
		if b.Collapsed {
			arrow = " " + sty.Subtle.Render("[+]")
		} else {
			arrow = " " + sty.Subtle.Render("[-]")
		}
	}
	if running {
		arrow = " " + sty.Subtle.Render("…")
	}

	header := fmt.Sprintf("%s %s %s%s", icon, b.ToolName, summary, arrow)
	accentLine := "  " + toolAccent.Render(header)

	if b.Content == "" || b.Collapsed {
		return accentLine
	}

	contentW := width - 6
	if contentW < 10 {
		contentW = 10
	}
	rendered := renderToolContent(b, contentW, sty)
	indented := lipgloss.NewStyle().PaddingLeft(2).Render(rendered)
	return lipgloss.JoinVertical(lipgloss.Left, accentLine, indented)
}

// toolSummary 折叠行摘要。
func toolSummary(b ContentBlock) string {
	switch b.ToolName {
	case "read":
		return extractReadSummary(b.Content)
	case "edit":
		return extractEditSummary(b.Content)
	default:
		return b.ToolArgs
	}
}

func extractReadSummary(c string) string {
	err := extractTag(c, "<error>", "</error>")
	if err != "" {
		return err
	}
	path := extractTag(c, "<path>", "</path>")
	start := extractTag(c, `start="`, `"`)
	end := extractTag(c, `end="`, `"`)
	total := extractTag(c, `total="`, `"`)
	if path != "" && start != "" && end != "" {
		return fmt.Sprintf("%s  L%s-%s/%s", path, start, end, total)
	}
	if path != "" {
		return path
	}
	// 纯文本错误（如 File not found），取首行。
	if idx := strings.IndexByte(c, '\n'); idx >= 0 {
		return c[:idx]
	}
	return c
}

func extractEditSummary(c string) string {
	path := extractTag(c, "<path>", "</path>")
	occ := extractTag(c, "<occurrences>", "</occurrences>")
	if path != "" {
		return fmt.Sprintf("%s  ×%s", path, occ)
	}
	return ""
}

func extractTag(s, open, close string) string {
	start := strings.Index(s, open)
	if start == -1 {
		return ""
	}
	start += len(open)
	end := strings.Index(s[start:], close)
	if end == -1 {
		return ""
	}
	return s[start : start+end]
}

// renderToolContent 根据工具类型渲染内容。
func renderToolContent(b ContentBlock, contentW int, sty *styles.Styles) string {
	switch b.ToolName {
	case "read":
		return sty.Muted.MaxWidth(contentW).Render(ParseReadOutput(b.Content))
	case "edit":
		return renderBlockDiff(ContentBlock{Content: ParseEditOutput(b.Content)}, sty)
	default:
		return sty.Muted.MaxWidth(contentW).Render(b.Content)
	}
}
