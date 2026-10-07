// message_error.go — ErrorMessageItem：错误消息渲染（红色圆点 + 缩进，与系统消息的
// 圆点条目格式统一，仅圆点与内容为错误色）。
package message

import (
	"strings"

	"nekocode/interaction/tui/styles"

	"charm.land/lipgloss/v2"
)

type ErrorMessageItem struct {
	content string
	sty     *styles.Styles
	cache   cachedRender
}

func NewErrorMessageItem(sty *styles.Styles, content string) *ErrorMessageItem {
	return &ErrorMessageItem{content: content, sty: sty}
}

func (m *ErrorMessageItem) Render(width int) string {
	cw := fullMessageWidth(width)
	if m.cache.width == cw && m.cache.rendered != "" {
		return m.cache.rendered
	}
	contentW := max(cw-4, 10)
	body := strings.TrimSpace(RenderMarkdown(strings.TrimSpace(m.content), contentW))
	rendered := renderBulletBody(body, "  "+m.sty.Red.Render("•")+" ")
	out := lipgloss.NewStyle().Width(cw).MaxWidth(cw).Render(rendered)
	m.cache.rendered = out
	m.cache.width = cw
	m.cache.height = strings.Count(out, "\n") + 1
	return out
}

func (m *ErrorMessageItem) Height(width int) int {
	cw := fullMessageWidth(width)
	if m.cache.height > 0 && m.cache.width == cw {
		return m.cache.height
	}
	return strings.Count(m.content, "\n") + 2
}
