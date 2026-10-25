// messages.go — Messages 容器：管理消息列表、处理中状态、流式内容分发。
package components

import (
	"fmt"
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
	Processing         bool
	Follow             bool
	sty                *styles.Styles
	processingItem     *processing.ProcessingItem
	renderingSuspended bool
	pendingFollow      bool
	processingDirty    bool
	history            []message.ChatMessage
	historyNotice      *message.SystemMessageItem
	historyPageOlder   bool
	historyPageNewer   bool
	historyBefore      int
	historyEnd         int
	historyTotal       int
	onHistoryPage      func(bool) tea.Cmd
	mu                 sync.Mutex
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

// SetRenderingSuspended keeps receiving data while an overlay displays a
// snapshot. Resuming defers the accumulated scroll work until the next view.
func (m *Messages) SetRenderingSuspended(suspended bool) {
	m.renderingSuspended = suspended
}

func (m *Messages) followToBottom() {
	if m.renderingSuspended || m.processingDirty {
		m.pendingFollow = true
		return
	}
	m.pendingFollow = false
	m.List.ScrollToBottom()
}

func (m *Messages) flushPendingRendering() {
	if m.processingDirty {
		m.processingDirty = false
		if idx := len(m.Items()) - 1; idx >= 0 {
			m.InvalidateItem(idx)
		}
	}
	if m.pendingFollow {
		m.pendingFollow = false
		if m.Follow {
			m.List.ScrollToBottom()
		}
	}
}

func (m *Messages) Render() string {
	m.flushPendingRendering()
	return m.List.Render()
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
			m.followToBottom()
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
			m.followToBottom()
		}
	}
	m.mu.Unlock()
}

// MoveProcessingFrom keeps live output when a prepared history window replaces
// its container. The host calls this on the UI thread after preparation ends.
func (m *Messages) MoveProcessingFrom(previous *Messages) {
	if previous == nil || previous == m {
		return
	}
	previous.mu.Lock()
	item, suspended := previous.processingItem, previous.renderingSuspended
	previous.mu.Unlock()
	m.SetRenderingSuspended(suspended)
	if item == nil {
		return
	}
	previous.SetProcessing(false)
	m.mu.Lock()
	m.processingItem = item
	m.Processing = true
	m.AppendItems(item)
	if m.Follow {
		m.followToBottom()
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

// DiscardStreamText drops streamed text that was not an answer, so it never
// reaches the committed blocks.
func (m *Messages) DiscardStreamText() {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.DiscardStreamText() })
}

func (m *Messages) ProcessToolBlock(b block.ContentBlock) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.AddToolBlock(b) })
}

func (m *Messages) AddToolOutput(toolName, output string, isError bool, callID ...string) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.AddToolOutput(toolName, output, isError, callID...) })
}

func (m *Messages) UpdateToolPreview(toolName, preview string, callID ...string) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.UpdateToolPreview(toolName, preview, callID...) })
}

func (m *Messages) UpdateToolPreviewForCall(callID, subID, toolName, preview string, decision protocol.ToolDecision) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) {
		p.UpdateToolPreviewForCall(callID, subID, toolName, preview, decision)
	})
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

func (m *Messages) AddSubToolOutput(subID, toolName, output string, isError bool, callID ...string) {
	m.UpdateProcessing(func(p *processing.ProcessingItem) { p.AddSubToolOutput(subID, toolName, output, isError, callID...) })
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
	m.processingDirty = true
	if m.Follow {
		m.pendingFollow = true
	}
	if !m.renderingSuspended {
		m.flushPendingRendering()
	}
}

const restoredHistoryPageSize = 100

// ReplaceMessages displays the newest page without rendering the entire
// transcript. Older messages remain available by scrolling to the top.
func (m *Messages) ReplaceMessages(messages []message.ChatMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.historyBefore, m.historyEnd, m.historyTotal = 0, 0, 0
	start := max(0, len(messages)-restoredHistoryPageSize)
	m.history = append([]message.ChatMessage(nil), messages[:start]...)
	m.historyNotice = nil
	m.onHistoryPage = nil
	items := make([]Item, 0, len(messages)-start+1)
	if start > 0 {
		m.historyNotice = message.NewSystemMessageItem(m.sty, "")
		m.updateHistoryNotice()
		items = append(items, m.historyNotice)
	}
	for _, msg := range messages[start:] {
		items = append(items, m.messageItem(msg))
	}
	m.Processing = false
	m.processingItem = nil
	m.SetItems(items...)
	m.Follow = true
	m.followToBottom()
}

func (m *Messages) updateHistoryNotice() {
	text := "已加载全部历史消息。"
	if len(m.history) > 0 {
		text = fmt.Sprintf("还有 %d 条更早的消息，继续向上滚动加载。", len(m.history))
	}
	m.historyNotice.SetRenderedContent(text)
}

// LoadOlderMessages inserts one page above the current viewport, retaining
// the rendered cache and the viewport's anchor rather than jumping to bottom.
func (m *Messages) LoadOlderMessages() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.history) == 0 {
		return false
	}
	start := max(0, len(m.history)-restoredHistoryPageSize)
	count := len(m.history) - start
	for i := len(m.history) - 1; i >= start; i-- {
		m.InsertItem(1, m.messageItem(m.history[i]))
	}
	clear(m.history[start:])
	m.history = m.history[:start]
	m.updateHistoryNotice()
	m.InvalidateItem(0)
	if m.offsetIdx == 0 {
		m.offsetIdx = count + 1
		m.offsetLine = 0
	}
	m.recomputePixelsAbove()
	m.Follow = false
	return true
}

func (m *Messages) scrollHistoryBy(lines int) {
	if lines < 0 && m.offsetIdx <= 1 && m.offsetLine == 0 {
		m.LoadOlderMessages()
	}
	m.ScrollBy(lines)
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
		m.followToBottom()
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
	m.followToBottom()
	m.SetFollow(true)
}

func (m *Messages) Update(msg tea.Msg) (*Messages, tea.Cmd) {
	if m.onHistoryPage != nil {
		direction := 0
		switch event := msg.(type) {
		case tea.KeyPressMsg:
			switch event.String() {
			case "up", "pgup":
				direction = -1
			case "down", "pgdown":
				direction = 1
			}
		case tea.MouseMsg:
			switch event.Mouse().Button {
			case tea.MouseWheelUp:
				direction = -1
			case tea.MouseWheelDown:
				direction = 1
			}
		}
		if direction < 0 && m.historyPageOlder && m.offsetIdx == 0 && m.offsetLine == 0 {
			return m, m.onHistoryPage(true)
		}
		if direction > 0 && m.historyPageNewer && m.AtBottom() {
			return m, m.onHistoryPage(false)
		}
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up":
			m.scrollHistoryBy(-1)
			if m.Processing {
				m.SetFollow(false)
			}
		case "down":
			m.ScrollBy(1)
		case "pgup":
			m.scrollHistoryBy(-m.Height())
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
			m.scrollHistoryBy(-3)
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

// ReplaceHistoryWindow holds only one disk-backed window. Moving beyond its
// edges asks the host for another window rather than accumulating old pages.
func (m *Messages) ReplaceHistoryWindow(messages []message.ChatMessage, before, end, total int, older, newer bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.historyBefore, m.historyEnd, m.historyTotal = before, end, total
	m.history = nil
	m.historyNotice = nil
	m.onHistoryPage = nil
	items := make([]Item, 0, len(messages))
	for _, msg := range messages {
		items = append(items, m.messageItem(msg))
	}
	m.Processing = false
	m.processingItem = nil
	m.SetItems(items...)
	m.Follow = !newer
	m.historyPageOlder = older
	m.historyPageNewer = newer
	m.followToBottom()
}

func (m *Messages) SetHistoryPageHandler(handler func(bool) tea.Cmd) { m.onHistoryPage = handler }

// HistoryWindowHint describes navigation state, never conversation content.
func (m *Messages) HistoryWindowHint() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Processing || (!m.historyPageOlder && !m.historyPageNewer) || m.historyTotal <= 0 {
		return ""
	}
	hint := fmt.Sprintf("记录 %d–%d，共 %d 条", m.historyBefore, m.historyEnd, m.historyTotal)
	if m.historyPageOlder {
		hint += " · ↑ 更早"
	}
	if m.historyPageNewer {
		hint += " · ↓ 较新"
	}
	return hint
}

// ScrollbarMetrics projects the loaded window onto the whole transcript.
// Unloaded line heights are unknown, so size is estimated from this window;
// position uses stable raw sequence boundaries rather than restarting per page.
func (m *Messages) ScrollbarMetrics() (totalHeight, viewportHeight int, percent float64) {
	m.flushPendingRendering()
	totalHeight, viewportHeight = m.TotalContentHeight(), m.Height()
	percent = m.List.ScrollPercent()
	span := m.historyEnd - m.historyBefore + 1
	if m.historyBefore < 1 || span < 1 || m.historyTotal < m.historyEnd {
		return
	}
	start := float64(m.historyBefore-1) / float64(m.historyTotal)
	end := float64(m.historyEnd) / float64(m.historyTotal)
	// A short page has no local scroll offset. Prefer its newest boundary,
	// matching ReplaceHistoryWindow's initial bottom-aligned viewport.
	if totalHeight <= viewportHeight {
		percent = 1
	}
	percent = start + (end-start)*percent
	totalHeight = max(totalHeight, int(float64(max(totalHeight, 1))*float64(m.historyTotal)/float64(span)))
	if m.historyPageOlder || m.historyPageNewer {
		totalHeight = max(totalHeight, viewportHeight+1)
	}
	return
}

// HistoryWindowBytes conservatively counts source and rendered string bytes.
// It is a cache budget, not a measurement of total process RSS.
func (m *Messages) HistoryWindowBytes(messages []message.ChatMessage) int {
	n := 0
	for _, msg := range messages {
		n += len(msg.Content) + len(msg.RenderedContent) + len(msg.Reasoning)
		for _, b := range msg.Blocks {
			n += len(b.Content) + len(b.ToolArgs)
		}
	}
	m.cacheMu.RLock()
	defer m.cacheMu.RUnlock()
	for _, item := range m.cache {
		for _, line := range item.lines {
			n += len(line)
		}
	}
	return n
}
