// messages.go — Messages 容器：管理消息列表、处理中状态、流式内容分发。
package components

import (
	"sync"

	"nekocode/tui/components/block"
	"nekocode/tui/components/processing"
	"nekocode/tui/components/message"
	"nekocode/tui/styles"

	tea "charm.land/bubbletea/v2"
)

type Messages struct {
	*List
	Processing     bool
	Follow         bool
	sty            *styles.Styles
	processingItem *processing.ProcessingItem
	mu             sync.Mutex
}

func NewMessages(width, height int, sty *styles.Styles) *Messages {
	l := NewList()
	l.SetSize(width, height)
	l.SetGap(1)

	return &Messages{
		List:   l,
		Follow: true,
		sty:    sty,
	}
}

func (m *Messages) SetSize(width, height int) {
	m.List.SetSize(width, height)
}

func (m *Messages) SetProcessing(on bool) {
	m.mu.Lock()
	m.Processing = on
	if on && m.processingItem == nil {
		m.processingItem = processing.NewProcessingItem(m.sty)
		m.AppendItems(m.processingItem)
		if m.Follow {
			m.ScrollToBottom()
		}
	} else if !on && m.processingItem != nil {
		items := m.Items()
		m.SetItems()
		for _, item := range items {
			if _, ok := item.(*processing.ProcessingItem); !ok {
				m.AppendItems(item)
			}
		}
		m.processingItem = nil
	}
	m.mu.Unlock()
}

func (m *Messages) SetSpinnerView(view string) {
	m.mu.Lock()
	if m.processingItem != nil {
		m.processingItem.SetSpinnerView(view)
		m.invalidateProcessing()
	}
	m.mu.Unlock()
}

func (m *Messages) SetSkill(s string) {
	m.mu.Lock()
	if m.processingItem != nil {
		m.processingItem.SetSkill(s)
		m.invalidateProcessing()
	}
	m.mu.Unlock()
}

func (m *Messages) SetProcessingStatus(text string) {
	m.mu.Lock()
	if m.processingItem != nil {
		m.processingItem.SetStatusText(text)
		m.invalidateProcessing()
	}
	m.mu.Unlock()
}


func (m *Messages) SetBlocks(blocks []block.ContentBlock) {
	m.mu.Lock()
	if m.processingItem != nil {
		m.processingItem.SetBlocks(blocks)
		m.invalidateProcessing()
	}
	m.mu.Unlock()
}

func (m *Messages) SetTodos(text string) {
	m.mu.Lock()
	if m.processingItem != nil {
		m.processingItem.SetTodos(text)
		m.invalidateProcessing()
	}
	m.mu.Unlock()
}

func (m *Messages) ProcessStreamText(delta string) {
	m.mu.Lock()
	if m.processingItem != nil {
		m.processingItem.AppendStreamText(delta)
		m.invalidateProcessing()
	}
	m.mu.Unlock()
}

func (m *Messages) ProcessReasoningText(delta string) {
	m.mu.Lock()
	if m.processingItem != nil {
		m.processingItem.AppendReasoningText(delta)
		m.invalidateProcessing()
	}
	m.mu.Unlock()
}

func (m *Messages) ProcessToolBlock(b block.ContentBlock) {
	m.mu.Lock()
	if m.processingItem != nil {
		m.processingItem.AddToolBlock(b)
		m.invalidateProcessing()
	}
	m.mu.Unlock()
}

func (m *Messages) AddDiffBlock(content string) {
	m.mu.Lock()
	if m.processingItem != nil {
		m.processingItem.AddDiffBlock(content)
		m.invalidateProcessing()
	}
	m.mu.Unlock()
}

func (m *Messages) AddTaskOutput(output string) {
	m.mu.Lock()
	if m.processingItem != nil {
		m.processingItem.AddTaskOutput(output)
		m.invalidateProcessing()
	}
	m.mu.Unlock()
}

func (m *Messages) AddToolOutput(toolName, output string) {
	m.mu.Lock()
	if m.processingItem != nil {
		m.processingItem.AddToolOutput(toolName, output)
		m.invalidateProcessing()
	}
	m.mu.Unlock()
}

func (m *Messages) AddThinkBlock(content string) {
	m.mu.Lock()
	if m.processingItem != nil {
		m.processingItem.AddThinkBlock(content)
		m.invalidateProcessing()
	}
	m.mu.Unlock()
}

func (m *Messages) UpdateProcessing(fn func(p *processing.ProcessingItem)) {
	m.mu.Lock()
	if m.processingItem != nil {
		fn(m.processingItem)
		m.invalidateProcessing()
	}
	m.mu.Unlock()
}

func (m *Messages) ClearProcessing() {
	m.mu.Lock()
	if m.processingItem != nil {
		m.processingItem.Clear()
		m.invalidateProcessing()
	}
	m.mu.Unlock()
}

func (m *Messages) ProcessingBlocks() []block.ContentBlock {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.processingItem != nil {
		return m.processingItem.Blocks()
	}
	return nil
}

func (m *Messages) invalidateProcessing() {
	idx := len(m.Items()) - 1
	if idx >= 0 {
		m.InvalidateItem(idx)
	}
	if m.Follow {
		m.ScrollToBottom()
	}
}

func (m *Messages) AddMessage(msg message.ChatMessage) {
	var item Item
	switch msg.Role {
	case "user":
		item = message.NewUserMessageItem(m.sty, msg.Content)
	case "assistant":
		a := message.NewAssistantMessageItem(m.sty, msg.Content)
		if msg.RenderedContent != "" {
			a.SetRenderedContent(msg.RenderedContent)
		}
		a.SetBlocks(msg.Blocks)
		if msg.Footer != "" {
			a.SetFooter(msg.Footer)
		}
		item = a
	case "system":
		s := message.NewSystemMessageItem(m.sty, msg.Content)
		if msg.Title != "" {
			s.SetTitle(msg.Title)
		}
		if msg.RenderedContent != "" {
			s.SetRenderedContent(msg.RenderedContent)
		}
		item = s
	case "error":
		item = message.NewErrorMessageItem(m.sty, msg.Content)
	default:
		item = message.NewUserMessageItem(m.sty, msg.Content)
	}
	m.AppendItems(item)
	if m.Follow {
		m.ScrollToBottom()
	}
}

func (m *Messages) SetFollow(follow bool) {
	m.mu.Lock()
	m.Follow = follow
	m.mu.Unlock()
}

func (m *Messages) GotoBottom() {
	m.ScrollToBottom()
	m.SetFollow(true)
}

// ToggleLastAssistant 展开/折叠最后一个助手消息的工具块。
func (m *Messages) ToggleLastAssistant() {
	items := m.Items()
	for i := len(items) - 1; i >= 0; i-- {
		if a, ok := items[i].(*message.AssistantMessageItem); ok {
			if a.ToggleAny() {
				m.InvalidateItem(i)
				return
			}
		}
	}
}


func (m *Messages) Update(msg tea.Msg) (*Messages, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up":
			m.ScrollBy(-1)
			if m.Processing { m.SetFollow(false) }
		case "down":
			m.ScrollBy(1)
		case "pgup":
			m.ScrollBy(-m.Height())
			if m.Processing { m.SetFollow(false) }
		case "pgdown":
			m.ScrollBy(m.Height())
		}
	case tea.MouseMsg:
		mev := msg.Mouse()
		switch mev.Button {
		case tea.MouseWheelUp:
			m.ScrollBy(-3)
			if m.Processing { m.SetFollow(false) }
		case tea.MouseWheelDown:
			m.ScrollBy(3)
		}
	}

	if m.AtBottom() {
		m.SetFollow(true)
	} else if !m.Processing {
		m.SetFollow(false)
	}
	// During processing: if user scrolled away, preserve their choice.
	// If they scroll back to bottom (AtBottom), re-enable follow.

	return m, nil
}

