// message_assistant.go — 助手消息渲染。
package message

import (
	"strings"
	"sync"

	"nekocode/tui/components/block"
	"nekocode/tui/styles"

	"charm.land/lipgloss/v2"
)

type AssistantMessageItem struct {
	content         string
	renderedContent string
	footer          string
	blocks          []block.ContentBlock
	sty             *styles.Styles
	cache           cachedRender
	mu              sync.Mutex
}

// ToggleAny 展开最后一个折叠的工具块；全展开时折叠最后一个。无工具块返回 false。
func (m *AssistantMessageItem) ToggleAny() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	lastCollapsed, lastExpanded := -1, -1
	for i := range m.blocks {
		if m.blocks[i].Type != block.BlockTool || m.blocks[i].Content == "" {
			continue
		}
		if m.blocks[i].Collapsed {
			lastCollapsed = i
		}
		if !m.blocks[i].Collapsed {
			lastExpanded = i
		}
	}
	target := lastCollapsed
	if target < 0 {
		target = lastExpanded
	}
	if target < 0 {
		return false
	}
	m.blocks[target].Collapsed = !m.blocks[target].Collapsed
	m.cache = cachedRender{}
	return true
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

func (m *AssistantMessageItem) SetBlocks(blocks []block.ContentBlock) {
	m.mu.Lock()
	// 默认最后一个工具块展开。
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].Type == block.BlockTool && blocks[i].Content != "" {
			blocks[i].Collapsed = false
			break
		}
	}
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

func (m *AssistantMessageItem) Render(width int) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	cw := cappedWidth(width)
	contentW := cw - barOverhead

	header := m.sty.Primary.Bold(true).Render("Assistant")
	msgParts := []string{header, ""}

	if len(m.blocks) > 0 {
		cards := block.RenderTools(m.blocks, contentW, m.sty)
		if cards != "" {
			msgParts = append(msgParts, cards)
		}
	}

	raw := m.content
	if m.renderedContent != "" {
		raw = m.renderedContent
	}
	body := strings.TrimSpace(RenderMarkdown(strings.TrimSpace(raw), contentW))
	if body != "" {
		if len(m.blocks) > 0 {
			msgParts = append(msgParts, "")
		}
		msgParts = append(msgParts, body)
	}
	if m.footer != "" {
		msgParts = append(msgParts, "", styles.SubtleStyle.Render(m.footer))
	}

	msgBlock := thickLeftBar(stripLeadingSpaces(strings.TrimSpace(lipgloss.JoinVertical(lipgloss.Left, msgParts...))), lipgloss.Color("#4ec9b0"), cw)

	m.cache.rendered = msgBlock
	m.cache.width = cw
	m.cache.height = len(strings.Split(msgBlock, "\n"))
	return msgBlock
}

func (m *AssistantMessageItem) Height(width int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	cw := cappedWidth(width)
	if m.cache.height > 0 && m.cache.width == cw {
		return m.cache.height
	}
	// 缓存无效时执行一次渲染来计算真实高度。
	m.mu.Unlock()
	_ = m.Render(width)
	m.mu.Lock()
	return m.cache.height
}
