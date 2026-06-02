package block

import (
	"fmt"
	"strings"

	"nekocode/tui/styles"

	"charm.land/lipgloss/v2"
)

func renderToolLine(b ContentBlock, width int, sty *styles.Styles) string {
	running := !b.Done && b.Content == ""

	summary := b.ToolArgs
	if b.Content != "" && b.Collapsed {
		summary = toolSummary(b)
	}

	arrow := ""
	if running {
		arrow = " " + sty.Subtle.Render("…")
	} else if b.Content != "" {
		if b.Collapsed {
			arrow = " " + sty.Subtle.Render("[+]")
		} else {
			arrow = " " + sty.Subtle.Render("[-]")
		}
	}

	header := fmt.Sprintf("◉ %s %s%s", b.ToolName, summary, arrow)
	accentLine := "  " + toolAccent.Render(header)

	if running || b.Collapsed {
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

func toolSummary(b ContentBlock) string {
	switch b.ToolName {
	case "read":
		return extractReadSummary(b.Content)
	default:
		return b.ToolArgs
	}
}

func extractReadSummary(c string) string {
	err := extractTag(c, "<error>", "</error>")
	if err != "" { return err }
	path := extractTag(c, "<path>", "</path>")
	start := extractTag(c, `start="`, `"`)
	end := extractTag(c, `end="`, `"`)
	total := extractTag(c, `total="`, `"`)
	if path != "" && start != "" && end != "" {
		return fmt.Sprintf("%s  L%s-%s/%s", path, start, end, total)
	}
	if path != "" { return path }
	if idx := strings.IndexByte(c, '\n'); idx >= 0 { return c[:idx] }
	return c
}

func renderEditPreview(content string, width int, sty *styles.Styles) string {
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("#98c379"))
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("#e06c75"))
	subtle := lipgloss.NewStyle().Foreground(lipgloss.Color("#5c6370"))
	numStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#666"))

	var out strings.Builder
	for _, line := range strings.Split(content, "\n") {
		prefix := byte(' ')
		if len(line) > 0 && (line[0] == '-' || line[0] == '+' || line[0] == ' ') {
			prefix = line[0]
			line = line[1:]
		}
		lineNo := 0
		text := line
		if colon := strings.IndexByte(line, ':'); colon > 0 {
			fmt.Sscanf(line[:colon], "%d", &lineNo)
			rest := line[colon+1:]
			if rb := strings.IndexByte(rest, ']'); rb > 0 {
				text = rest[rb+1:]
			}
		}
		if lineNo > 0 {
			out.WriteString(numStyle.Render(pad4(lineNo)))
			switch prefix {
			case '-':
				out.WriteString(red.Render("- "))
			case '+':
				out.WriteString(green.Render("+ "))
			default:
				out.WriteString(subtle.Render("  "))
			}
		}
		switch prefix {
		case '-':
			out.WriteString(red.Render(text))
		case '+':
			out.WriteString(green.Render(text))
		default:
			out.WriteString(subtle.Render(text))
		}
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n")
}

func pad4(n int) string {
	s := fmt.Sprintf("%d", n)
	for len(s) < 4 {
		s += " "
	}
	return s + " "
}

func extractTag(s, open, close string) string {
	start := strings.Index(s, open)
	if start == -1 { return "" }
	start += len(open)
	end := strings.Index(s[start:], close)
	if end == -1 { return "" }
	return s[start : start+end]
}

func renderToolContent(b ContentBlock, contentW int, sty *styles.Styles) string {
	switch b.ToolName {
	case "read":
		return sty.Muted.MaxWidth(contentW).Render(ParseReadOutput(b.Content))
	case "edit":
		return renderEditPreview(b.Content, contentW, sty)
	case "bash":
		c := strings.TrimSpace(b.Content)
		if c == "" {
			return sty.Subtle.Render("(No output)")
		}
		lines := strings.Split(c, "\n")
		if len(lines) <= 3 {
			return sty.Muted.MaxWidth(contentW).Render(c)
		}
		head := strings.Join(lines[:3], "\n")
		tail := sty.Subtle.Render(fmt.Sprintf("\n... (%d more lines)", len(lines)-3))
		return sty.Muted.MaxWidth(contentW).Render(head) + tail
	default:
		return sty.Muted.MaxWidth(contentW).Render(b.Content)
	}
}
