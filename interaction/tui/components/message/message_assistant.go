// message_assistant.go — 助手消息渲染。
package message

import (
	"strings"
	"sync"

	"nekocode/interaction/tui/components/block"
	"nekocode/interaction/tui/styles"
	"nekocode/protocol"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type AssistantMessageItem struct {
	content         string
	renderedContent string
	reasoning       string
	footer          string
	telemetry       *protocol.Metrics
	blocks          []block.ContentBlock
	sty             *styles.Styles
	cache           cachedRender
	mu              sync.Mutex
}

func NewAssistantMessageItem(sty *styles.Styles, content string) *AssistantMessageItem {
	return &AssistantMessageItem{content: content, sty: sty}
}

func (m *AssistantMessageItem) SetRenderedContent(content string) {
	m.mu.Lock()
	m.renderedContent = content
	m.cache = cachedRender{}
	m.mu.Unlock()
}

func (m *AssistantMessageItem) SetReasoning(reasoning string) {
	m.mu.Lock()
	m.reasoning = reasoning
	m.cache = cachedRender{}
	m.mu.Unlock()
}

func (m *AssistantMessageItem) SetBlocks(blocks []block.ContentBlock) {
	m.mu.Lock()
	m.blocks = blocks
	m.cache = cachedRender{}
	m.mu.Unlock()
}

func (m *AssistantMessageItem) Blocks() []block.ContentBlock {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.blocks
}

func (m *AssistantMessageItem) SetFooter(footer string) {
	m.mu.Lock()
	m.footer = footer
	m.cache = cachedRender{}
	m.mu.Unlock()
}

func (m *AssistantMessageItem) SetTelemetry(telemetry protocol.Metrics) {
	m.mu.Lock()
	m.telemetry = &telemetry
	m.cache = cachedRender{}
	m.mu.Unlock()
}

func (m *AssistantMessageItem) Render(width int) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	cw := fullMessageWidth(width)
	contentW := cw

	sepW := contentW
	separator := m.sty.Border.Render(strings.Repeat("─", sepW))
	msgParts := []string{separator}

	if len(m.blocks) > 0 {
		cards := block.RenderTools(m.blocks, contentW, m.sty)
		if cards != "" {
			msgParts = append(msgParts, cards)
		}
	}
	if reasoning := strings.TrimSpace(m.reasoning); reasoning != "" {
		thinking := strings.TrimSpace(RenderMarkdown(reasoning, max(contentW-4, 10)))
		if thinking != "" {
			msgParts = append(msgParts,
				"  "+m.sty.Muted.Render("thinking"),
				m.sty.Muted.Render(indentLines(thinking, "    ")),
			)
		}
	}

	raw := m.content
	if m.renderedContent != "" {
		raw = m.renderedContent
	}
	bodyW := max(contentW-4, 10)
	body := strings.TrimSpace(RenderMarkdown(strings.TrimSpace(raw), bodyW))
	if body != "" {
		if len(m.blocks) > 0 {
			msgParts = append(msgParts, "")
		}
		msgParts = append(msgParts, renderAssistantBody(body, m.sty))
	}
	if m.footer != "" {
		msgParts = append(msgParts, "", "  "+styles.SubtleStyle.Render(m.footer))
	}
	if m.telemetry != nil {
		msgParts = append(msgParts, "", renderTelemetryFooter(*m.telemetry, contentW, m.sty))
	}

	msgBlock := lipgloss.NewStyle().
		Width(cw).
		MaxWidth(cw).
		Render(strings.TrimSpace(lipgloss.JoinVertical(lipgloss.Left, msgParts...)))

	m.cache.rendered = msgBlock
	m.cache.width = cw
	m.cache.height = len(strings.Split(msgBlock, "\n"))
	return msgBlock
}

func indentLines(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (m *AssistantMessageItem) Height(width int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	cw := fullMessageWidth(width)
	if m.cache.height > 0 && m.cache.width == cw {
		return m.cache.height
	}
	// 缓存无效时执行一次渲染来计算真实高度。
	m.mu.Unlock()
	_ = m.Render(width)
	m.mu.Lock()
	return m.cache.height
}

func renderAssistantBody(body string, sty *styles.Styles) string {
	body = stripLeadingSpaces(body)
	lines := strings.Split(body, "\n")
	prefix := "  " + sty.Teal.Render("•") + " "
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
