// messages.go — Messages 容器：管理消息列表、处理中状态、流式内容分发。
package components

import (
	"sync"

	"nekocode/interaction/tui/components/block"
	"nekocode/interaction/tui/components/message"
	"nekocode/interaction/tui/components/processing"
	"nekocode/interaction/tui/styles"
	"nekocode/protocol"

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
	follow := m.Follow
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
			if active, ok := item.(*processing.ProcessingItem); ok {
				if history := active.CompactionHistory(); history != nil {
					m.AppendItems(history)
				}
			} else {
				m.AppendItems(item)
			}
		}
		m.processingItem = nil
		if follow {
			m.ScrollToBottom()
		}
	}
	m.mu.Unlock()
}

func (m *Messages) SetSpinnerView(view string) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.SetSpinnerView(view) })
}

func (m *Messages) SetSkill(s string) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.SetSkill(s) })
}

func (m *Messages) SetProcessingStatus(text string) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.SetStatusText(text) })
}

func (m *Messages) SetBlocks(blocks []block.ContentBlock) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.SetBlocks(blocks) })
}

func (m *Messages) SetTodos(text string) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.SetTodos(text) })
}

func (m *Messages) ProcessStreamText(delta string) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.AppendStreamText(delta) })
}

func (m *Messages) ProcessThinkingText(delta string) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.AppendThinkingText(delta) })
}

func (m *Messages) ProcessToolBlock(b block.ContentBlock) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.AddToolBlock(b) })
}

func (m *Messages) AddToolOutput(toolName, output string, isError bool) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.AddToolOutput(toolName, output, isError) })
}

func (m *Messages) UpdateToolPreview(toolName, preview string) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.UpdateToolPreview(toolName, preview) })
}

func (m *Messages) AccumulatedText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.processingItem != nil {
		return m.processingItem.OutputText()
	}
	return ""
}

func (m *Messages) AddThinkBlock(content string) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.AddThinkBlock(content) })
}

func (m *Messages) UpdateProcessing(fn func(p *processing.ProcessingItem)) {
	m.mu.Lock()
	if m.processingItem != nil {
		fn(m.processingItem)
		m.invalidateProcessing()
	}
	m.mu.Unlock()
}

func (m *Messages) AddSubAgent(id, subType string, colorIdx int) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.AddSubAgent(id, subType, colorIdx) })
}

func (m *Messages) RemoveSubAgent(id string) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.RemoveSubAgent(id) })
}

func (m *Messages) AddSubToolOutput(subID, toolName, output string, isError bool) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.AddSubToolOutput(subID, toolName, output, isError) })
}

func (m *Messages) ClearProcessing() {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.Clear() })
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
	m.mu.Lock()
	defer m.mu.Unlock()

	item := m.messageItem(msg)
	if m.processingItem != nil {
		// Keep the processing item pinned as the last list item — both
		// invalidateProcessing and the tick animation assume it is final.
		m.InsertItem(len(m.Items())-1, item)
	} else {
		m.AppendItems(item)
	}
	if m.Follow {
		m.ScrollToBottom()
	}
}

func (m *Messages) messageItem(msg message.ChatMessage) Item {
	switch msg.Role {
	case "user":
		return message.NewUserMessageItem(m.sty, msg.Content)
	case "assistant":
		a := message.NewAssistantMessageItem(m.sty, msg.Content)
		a.SetReasoning(msg.Reasoning)
		if msg.RenderedContent != "" {
			a.SetRenderedContent(msg.RenderedContent)
		}
		a.SetBlocks(msg.Blocks)
		if msg.Footer != "" {
			a.SetFooter(msg.Footer)
		}
		if msg.Telemetry != nil {
			a.SetTelemetry(*msg.Telemetry)
		}
		return a
	case "telemetry":
		if msg.Telemetry == nil {
			return message.NewTelemetryMessageItem(m.sty, protocol.Metrics{})
		}
		return message.NewTelemetryMessageItem(m.sty, *msg.Telemetry)
	case "system":
		s := message.NewSystemMessageItem(m.sty, msg.Content)
		if msg.Title != "" {
			s.SetTitle(msg.Title)
		}
		if msg.RenderedContent != "" {
			s.SetRenderedContent(msg.RenderedContent)
		}
		return s
	case "error":
		return message.NewErrorMessageItem(m.sty, msg.Content)
	default:
		return message.NewUserMessageItem(m.sty, msg.Content)
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

func (m *Messages) Update(msg tea.Msg) (*Messages, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up":
			m.ScrollBy(-1)
			if m.Processing {
				m.SetFollow(false)
			}
		case "down":
			m.ScrollBy(1)
		case "pgup":
			m.ScrollBy(-m.Height())
			if m.Processing {
				m.SetFollow(false)
			}
		case "pgdown":
			m.ScrollBy(m.Height())
		}
	case tea.MouseMsg:
		mev := msg.Mouse()
		switch mev.Button {
		case tea.MouseWheelUp:
			m.ScrollBy(-3)
			if m.Processing {
				m.SetFollow(false)
			}
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
