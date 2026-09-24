// message_system.go — SystemMessageItem：系统消息渲染（灰色圆点 + 缩进，与对话块格式统一）。
package message

import (
	"net/url"
	"strings"

	"nekocode/interaction/tui/styles"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type SystemMessageItem struct {
	content         string
	renderedContent string
	title           string
	sty             *styles.Styles
	cache           cachedRender
}

func NewSystemMessageItem(sty *styles.Styles, content string) *SystemMessageItem {
	return &SystemMessageItem{content: content, sty: sty}
}

func (m *SystemMessageItem) SetTitle(title string) {
	m.title = title
	m.cache = cachedRender{}
}

func (m *SystemMessageItem) SetRenderedContent(content string) {
	m.renderedContent = content
	m.cache = cachedRender{}
}

func (m *SystemMessageItem) Render(width int) string {
	cw := fullMessageWidth(width)
	if m.cache.width == cw && m.cache.rendered != "" {
		return m.cache.rendered
	}
	contentW := max(cw-4, 10)
	content := m.renderedContent
	if content == "" {
		content = renderSystemContent(m.content, contentW, m.sty)
	} else {
		// Pre-rendered output (e.g. local command results) skips markdown;
		// bare URL lines still become terminal hyperlinks.
		content = hyperlinkURLLines(content, m.sty)
	}
	content = colorizeContextGlyphs(content)
	if m.title != "" {
		content = m.title + "\n" + content
	}
	sepW := cw
	separator := m.sty.Border.Render(strings.Repeat("─", sepW))
	body := lipgloss.NewStyle().Width(cw).MaxWidth(cw).Render(renderSystemBody(content, m.sty))
	out := separator + "\n" + body
	m.cache.rendered = out
	m.cache.width = cw
	m.cache.height = strings.Count(out, "\n") + 1
	return out
}

// renderSystemContent runs markdown over prose lines but renders bare URL
// lines (e.g. OAuth authorization links) as terminal hyperlinks — a single
// actionable line instead of a hard-wrapped URL blob.
func renderSystemContent(content string, width int, sty *styles.Styles) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	var out, prose []string
	flush := func() {
		if len(prose) > 0 {
			out = append(out, RenderMarkdown(strings.Join(prose, "\n"), width))
			prose = nil
		}
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isBareURL(trimmed) {
			flush()
			out = append(out, terminalHyperlink(sty, trimmed, "打开授权链接 ↗"))
			continue
		}
		prose = append(prose, line)
	}
	flush()
	return strings.Join(out, "\n")
}

// hyperlinkURLLines replaces bare URL lines with terminal hyperlinks while
// leaving every other line (including ANSI-styled ones) untouched.
func hyperlinkURLLines(content string, sty *styles.Styles) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isBareURL(trimmed) {
			lines[i] = terminalHyperlink(sty, trimmed, "打开授权链接 ↗")
		}
	}
	return strings.Join(lines, "\n")
}

func isBareURL(s string) bool {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return false
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// LastBareURL returns the last bare-URL line in content (e.g. an
// authorization link emitted by a local command), or "".
func LastBareURL(content string) string {
	var found string
	for _, line := range strings.Split(content, "\n") {
		if t := strings.TrimSpace(line); isBareURL(t) {
			found = t
		}
	}
	return found
}

// terminalHyperlink wraps label in an OSC 8 sequence pointing at url.
// Terminals without hyperlink support render the label as plain text.
func terminalHyperlink(sty *styles.Styles, url, label string) string {
	return "\x1b]8;;" + url + "\x07" + sty.Primary.Render(label) + "\x1b]8;;\x07"
}

// renderSystemBody: 灰色圆点 + 缩进, 与 assistant 正文 (•) 格式统一, 仅颜色不同。
func renderSystemBody(body string, sty *styles.Styles) string {
	body = stripLeadingSpaces(body)
	lines := strings.Split(body, "\n")
	prefix := "  " + sty.Muted.Render("•") + " "
	continuation := "    "

	var out strings.Builder
	bulletWritten := false
	for _, line := range lines {
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		if strings.TrimSpace(ansi.Strip(line)) == "" {
			continue
		}
		if !bulletWritten {
			out.WriteString(prefix)
			bulletWritten = true
		} else {
			out.WriteString(continuation)
		}
		out.WriteString(line)
	}
	return strings.TrimRight(out.String(), "\n")
}

func (m *SystemMessageItem) Height(width int) int {
	cw := fullMessageWidth(width)
	if m.cache.height > 0 && m.cache.width == cw {
		return m.cache.height
	}
	return strings.Count(m.content, "\n") + 2
}
