package tui

import (
	"context"
	"fmt"
	controlruntime "nekocode/runtime"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"nekocode/interaction/tui/components"
	"nekocode/interaction/tui/components/message"
	"nekocode/interaction/tui/styles"
	"nekocode/protocol"
	"nekocode/util/attachment"
)

const queuedSourceID = "message-queue"

type removedMessage struct {
	draft *components.InputDraft
	index int
}
type messageQueue struct {
	items       []*components.InputDraft
	removed     []removedMessage
	selected    int
	managing    bool
	editor      *components.Input
	session     string
	settling    controlruntime.RunID
	awaiting    *components.InputDraft
	awaitingRun controlruntime.RunID
	needsEdit   *components.InputDraft
}

// A removed draft owns its files through undo. An editor borrows the original
// files until save, so cancelling edits cannot invalidate a queued attachment.
func releaseQueueImages(draft *components.InputDraft, keep []components.InputImageAttachment) {
	for _, img := range draft.Images() {
		if !slices.Contains(keep, img) {
			_ = attachment.DeleteImage(img.Path)
		}
	}
}

func (q *messageQueue) forgetRemoved() {
	for _, item := range q.removed {
		if item.draft == q.needsEdit {
			q.needsEdit = nil
		}
		releaseQueueImages(item.draft, nil)
	}
	q.removed = nil
}

func (m *Model) clearQueue() {
	for _, item := range m.queue.items {
		releaseQueueImages(item, nil)
	}
	m.queue.forgetRemoved()
	m.queue = messageQueue{}
}

// queueShortcutAvailable mirrors the focus precedence in Update.
func (m *Model) queueShortcutAvailable() bool {
	return !m.queue.managing && !m.sessionNameFocused() && !m.modeChanging && !m.modePicker &&
		(m.tasks == nil || !m.tasks.open) &&
		(m.state == stateProcessing || (m.state == stateReady && len(m.queue.items) > 0))
}

func (m *Model) handleQueueKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	q := &m.queue
	key := msg.String()
	if !q.managing {
		waiting := m.state == stateReady && len(q.items) > 0
		enqueue := key == "tab" && m.queueShortcutAvailable() || key == "enter" && waiting
		switch {
		case key == "shift+tab" && len(q.items) > 0:
			q.managing = true
			q.selected = len(q.items) - 1
			m.Suggestions.Hide()
			m.commandMenuBack = nil
		case enqueue && strings.TrimSpace(m.Input.Value()) != "":
			if !m.enqueueInputDraft() {
				return true, nil
			}
			m.Input.SetSending(m.state == stateProcessing)
			m.Suggestions.Hide()
			m.commandMenuBack = nil
		default:
			return false, nil
		}
	} else if q.editor != nil {
		switch key {
		case "ctrl+s":
			if m.queueEditorCanSave() {
				draft := q.editor.CaptureDraft()
				releaseQueueImages(q.items[q.selected], draft.Images())
				if q.items[q.selected] == q.needsEdit {
					q.needsEdit = nil
				}
				q.items[q.selected] = draft
				q.editor = nil
			}
		case "esc":
			q.editor = nil
		default:
			_, cmd := q.editor.Update(msg)
			m.resizeMessages()
			return true, cmd
		}
	} else {
		switch key {
		case "up":
			q.selected = max(0, q.selected-1)
		case "down":
			q.selected = max(0, min(len(q.items)-1, q.selected+1))
		case "enter":
			if len(q.items) > 0 {
				q.editor = components.NewDraftEditor(m.Width)
				q.editor.RestoreDraft(q.items[q.selected])
			}
		case "delete":
			if len(q.items) > 0 {
				q.removed = append(q.removed, removedMessage{q.items[q.selected], q.selected})
				q.items = slices.Delete(q.items, q.selected, q.selected+1)
				q.selected = max(0, min(q.selected, len(q.items)-1))
			}
		case "ctrl+z":
			if n := len(q.removed); n > 0 {
				last := q.removed[n-1]
				q.removed = slices.Delete(q.removed, n-1, n)
				q.items = slices.Insert(q.items, last.index, last.draft)
				q.selected = last.index
			}
		case "esc", "shift+tab":
			q.managing = false
			q.forgetRemoved()
		}
	}
	m.resizeMessages()
	return true, nil
}

// Move the draft and its image ownership out of the composer together.
func (m *Model) enqueueInputDraft() bool {
	q := &m.queue
	if owner, ok := m.Runtime.(interface{ CurrentSessionID() string }); ok {
		current := owner.CurrentSessionID()
		if len(q.items) == 0 && q.awaiting == nil {
			q.session = current
		} else if current != q.session {
			m.Messages.AddMessage(message.ChatMessage{Role: "system", Content: "会话已切换，消息已保留在输入框中。"})
			return false
		}
	}
	q.items = append(q.items, m.Input.CaptureDraft())
	m.Input.Reset()
	return true
}

// Run on the event loop after all state transitions (including deferred
// prompt restoration and history refresh), consuming at most one queue item.
func (m *Model) dispatchQueued() tea.Cmd {
	q := &m.queue
	if len(q.items) == 0 || q.managing || q.settling != "" || m.state != stateReady || m.sessionLoading || m.sessionPendingChat != nil || m.modeChanging || m.modePicker || m.sessionName != nil {
		return nil
	}
	if m.sessionHistoryPaging && m.sessionHistoryPage > 0 {
		return m.loadSessionWindow(0, 0)
	}
	if rt, ok := m.Runtime.(interface{ CurrentSessionID() string }); ok && rt.CurrentSessionID() != q.session {
		m.clearQueue()
		return nil
	}
	draft := q.items[0]
	if draft == q.needsEdit {
		return nil
	}
	value := draft.Value()
	images := inputImageAttachments(draft.Images())
	// Address TUI-only background commands by their captured session.
	// Bot commands use the same session-checked run admission as chat.
	local, cmd := m.backgroundCommand(value, &q.session)
	if !local {
		local, cmd = m.taskCommand(value)
	}
	if !local && protocol.IsSlashCommandInput(value) {
		if rt, ok := m.Runtime.(interface {
			ExecuteLocalCommandIfCurrent(context.Context, string, string) (string, controlruntime.LocalCommandResult, error)
		}); ok {
			out, status, err := rt.ExecuteLocalCommandIfCurrent(protocol.WithCommandStatusDisplay(context.Background()), q.session, value)
			if err != nil {
				m.Messages.AddMessage(message.ChatMessage{Role: "error", Content: err.Error()})
				q.managing = true
				q.selected = 0
				return nil
			}
			local, cmd = m.showLocalCommandResult(value, out, status)
		}
	}
	if !local {
		cmd = m.beginChat(value, images, true)
		if m.state != stateProcessing {
			q.managing = true
			q.selected = 0
			return nil
		}
	}
	if len(images) == 0 {
		m.rememberInput(value)
	}
	q.items = slices.Delete(q.items, 0, 1)
	if local && len(images) > 0 {
		m.restoreUnusedImages(draft)
	}
	m.resizeMessages()
	// Local commands have no terminal event to wake the next queued turn.
	if local && len(q.items) > 0 {
		return tea.Batch(cmd, func() tea.Msg { return queueAdvancedMsg{} })
	}
	return cmd
}

type queueAdvancedMsg struct{}

func (m *Model) visibleQueueRows() int {
	return min(len(m.queue.items), max(1, min(3, (m.Height-9)/3)))
}

// A single draft fits beside its label when no item needs attention.
func (m *Model) inlineQueuePreview() bool {
	q := &m.queue
	return len(q.items) == 1 && !q.managing && q.items[0] != q.needsEdit
}

const queueManageHint = "Shift+Tab 管理"

func (m *Model) inlineQueueWidth() int {
	return m.Width - lipgloss.Width("  待发送  "+queueManageHint+"    ")
}

func (m *Model) queueHeight() int {
	q := &m.queue
	if len(q.items) == 0 && !q.managing {
		return 0
	}
	h := 1 + m.visibleQueueRows()
	if m.inlineQueuePreview() {
		if m.inlineQueueWidth() >= 12 {
			h--
		}
	} else if _, _, wrap := m.queueHeading(); wrap {
		h++
	}
	if m.queueSpacing() {
		h++
	}
	if q.managing {
		h += len(m.queueHelp())
	}
	if q.editor != nil {
		h += q.editor.Height() - 4
	}
	return h
}

func (m *Model) queueEditorCanSave() bool {
	return strings.TrimSpace(m.queue.editor.Value()) != ""
}

func (m *Model) queueHelp() []string {
	actions := []string{"↑↓ 选择", "Enter 编辑", "Del 移除", "Esc 继续"}
	if m.queue.editor != nil {
		actions = []string{"Ctrl+S 保存", "Enter 换行", "Esc 放弃"}
		if !m.queueEditorCanSave() {
			actions[0] = "请输入消息"
		}
	} else if len(m.queue.removed) > 0 {
		actions = append([]string{"Ctrl+Z 撤销"}, actions...)
	}
	var lines []string
	line := ""
	for _, action := range actions {
		if line == "" {
			line = action
			continue
		}
		next := line + " · " + action
		if lipgloss.Width(next) > m.Width-2 {
			lines = append(lines, line)
			line = action
		} else {
			line = next
		}
	}
	return append(lines, line)
}

// Leave a breathing row before the controls when the transcript has room.
func (m *Model) queueSpacing() bool {
	return m.Height-m.Input.Height()-m.Header.Height()-m.visibleQueueRows()-contentMarginV >= 12 && m.queue.editor == nil
}

func queuePreview(text string, width int) string {
	// Stop at the visible prefix even for a very large multiline paste.
	var b strings.Builder
	space := false
	count := 0
	for _, r := range text {
		if unicode.IsSpace(r) {
			space = b.Len() > 0
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
		count++
		if count > width {
			break
		}
	}
	return ansi.Truncate(b.String(), max(1, width), "…")
}

func (m *Model) queueHeading() (heading, hint string, wrap bool) {
	q := &m.queue
	heading = fmt.Sprintf("  待发送 · %d 条", len(q.items))
	hint = queueManageHint
	if q.managing {
		hint = "已暂停"
	}
	if len(q.items) > 0 && q.items[0] == q.needsEdit {
		if q.managing {
			hint = "图片未发送 · 请编辑或移除"
		} else {
			heading += " · 待编辑"
		}
	}
	return heading, hint, lipgloss.Width(heading+"  "+hint) > m.Width
}

func (m *Model) queueView() (string, *tea.Cursor) {
	q := &m.queue
	if len(q.items) == 0 && !q.managing {
		return "", nil
	}
	width := max(1, m.Width)
	clip := func(s string) string { return ansi.Truncate(s, width, "…") }
	if m.inlineQueuePreview() {
		label := "  " + styles.TealStyle.Render("待发送") + "  "
		hint := styles.MutedStyle.Render(queueManageHint)
		var row string
		if room := m.inlineQueueWidth(); room >= 12 {
			preview := queuePreview(q.items[0].Preview(), room)
			row = label + preview + "  " + hint
		} else {
			preview := queuePreview(q.items[0].Preview(), width-lipgloss.Width(label))
			row = clip(label+preview) + "\n" + clip("  "+hint)
		}
		if m.queueSpacing() {
			row += "\n"
		}
		return row, nil
	}
	heading, hint, wrap := m.queueHeading()
	title := styles.TealStyle.Bold(true).Render(heading)
	shortcut := styles.MutedStyle.Render(hint)
	var rows []string
	if wrap {
		rows = []string{clip(title), clip("  " + shortcut)}
	} else {
		rows = []string{clip(title + "  " + shortcut)}
	}
	n := m.visibleQueueRows()
	start := 0
	if q.managing {
		start = max(0, q.selected-n+1)
	}
	var cursor *tea.Cursor
	for i := start; i < start+n; i++ {
		marker := "    "
		style := styles.MutedStyle
		if q.managing && i == q.selected {
			marker = "  › "
			style = styles.CatEyeStyle
		}
		prefix := "  · "
		if q.managing {
			prefix = fmt.Sprintf("%s%*d  ", marker, len(fmt.Sprint(len(q.items))), i+1)
		}
		preview := queuePreview(q.items[i].Preview(), width-lipgloss.Width(prefix))
		if q.editor != nil && i == q.selected {
			preview = "编辑中"
		}
		rows = append(rows, style.Render(clip(prefix+preview)))
		if q.editor != nil && i == q.selected {
			cursor = q.editor.Cursor()
			cursor.Y += len(rows) - 1 // Input.Cursor includes its normal top border.
			rows = append(rows, strings.Split(q.editor.TextView(), "\n")...)
		}
	}
	if m.queueSpacing() {
		rows = append(rows, "")
	}
	if q.managing {
		for _, line := range m.queueHelp() {
			rows = append(rows, styles.SubtleStyle.Render(clip("  "+line)))
		}
	}
	return strings.Join(rows, "\n"), cursor
}

// Admission starts a run before input preparation finishes. A failed
// preparation returns the unsent message to its original place in the queue.
func (m *Model) finishQueuedInput(ev controlruntime.Event) {
	q := &m.queue
	if q.awaiting == nil || q.awaitingRun != ev.RunID {
		return
	}
	draft := q.awaiting
	q.awaiting = nil
	q.awaitingRun = ""
	// Reconnection can deliver a terminal event without the earlier input
	// acknowledgement. The run store records acceptance before fan-out.
	// Live delivery is ordered. Only reconnect/cancellation events from the
	// runtime may require consulting the recorded state for this exact run.
	if ev.Source.Kind == "runtime" {
		var snapshot controlruntime.RunSnapshot
		var found bool
		if reader, ok := m.Runtime.(interface {
			LookupRun(controlruntime.RunID) (controlruntime.RunSnapshot, bool)
		}); ok {
			snapshot, found = reader.LookupRun(ev.RunID)
		}
		if found && snapshot.InputAccepted {
			return
		}
		if !found || snapshot.FinishedAt == nil {
			m.Messages.AddMessage(message.ChatMessage{Role: "system", Content: "事件流中断，暂时无法确认消息是否已接收。请核对会话记录后决定是否重发。"})
			if len(q.items) > 0 && !q.managing {
				q.managing = true
				q.selected = 0
			}
			return
		}
		if snapshot.Status == controlruntime.RunDone {
			ev.Type = controlruntime.EventRunDone
		}
	}
	if ev.Type == controlruntime.EventRunDone {
		if len(draft.Images()) > 0 {
			m.restoreUnusedImages(draft)
		}
		return
	}
	m.prependPausedDraft(draft)
}

func (m *Model) restoreUnusedImages(draft *components.InputDraft) {
	m.Messages.AddMessage(message.ChatMessage{Role: "system", Content: "命令已处理，图片未发送，草稿已保留。"})
	if !m.Input.HasContent() && !m.queue.managing {
		m.Input.RestoreDraft(draft)
		return
	}
	m.prependPausedDraft(draft)
	m.queue.needsEdit = draft
}

func (m *Model) prependPausedDraft(draft *components.InputDraft) {
	q := &m.queue
	q.items = slices.Insert(q.items, 0, draft)
	if q.managing {
		if len(q.items) > 1 {
			q.selected++
		}
		for i := range q.removed {
			q.removed[i].index++
		}
	} else {
		q.managing = true
		q.selected = 0
	}
}

// The terminal event precedes release of the foreground run lease.
func (m *Model) waitQueuedTurn(id controlruntime.RunID) tea.Cmd {
	if id == "" || (len(m.queue.items) == 0 && !m.queue.managing) {
		return nil
	}
	rt, ok := m.Runtime.(interface {
		WaitRun(context.Context, controlruntime.RunID) error
	})
	if !ok {
		return nil
	}
	m.queue.settling = id
	return func() tea.Msg { err := rt.WaitRun(context.Background(), id); return queueSettledMsg{id, err} }
}

type queueSettledMsg struct {
	id  controlruntime.RunID
	err error
}
