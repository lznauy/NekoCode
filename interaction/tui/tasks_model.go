package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"nekocode/interaction/tui/components"
	"nekocode/interaction/tui/components/processing"
	controlruntime "nekocode/runtime"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

const taskPollInterval = 500 * time.Millisecond

const taskActiveResyncInterval = 5 * time.Second
const taskIdleResyncInterval = 30 * time.Second

type taskOverlay struct {
	allSessions                       bool
	open                              bool
	background                        string
	renderedView                      string
	reuseView                         bool
	backgroundWidth, backgroundHeight int
	generation                        uint64
	sessionID                         string
	items                             []controlruntime.SubagentSummary
	selected                          string
	query                             string
	searching                         bool
	listOffset                        int
	listPage                          controlruntime.SubagentPage
	listRequest                       uint64
	listLoading                       bool
	listLoaded                        bool // background refreshes retain the settled empty state
	listDirty                         bool
	listQueriedAt                     time.Time
	listError                         string
	appliedQuery                      string
	appliedOffset                     int
	detail                            *taskDetail
	savedDetail                       *taskDetail
	streamCancel                      context.CancelFunc
	stream                            <-chan controlruntime.Event
	streamRequest                     uint64
	subscribeRetryAt                  time.Time
}

type taskDetail struct {
	fullPrompt          string
	fullPromptLoaded    bool
	id                  string
	info                controlruntime.SubagentSummary
	page                controlruntime.SubagentHistoryPage
	loadedCursor        int
	historyRetryBefore  int
	cursors             []int
	request             uint64
	loading             bool
	historyLoaded       bool
	err                 string
	follow              bool
	newRecords          int
	list                *components.List
	entries             []taskEntry
	tools               map[string]liveTaskTool
	dirty               bool
	inspector           *components.List
	inspectedKey        string
	inspectedEntry      *taskEntry
	output              *taskOutputWindow
	outputRequest       uint64
	attempt             *controlruntime.SubagentAttemptSummary
	attempts            controlruntime.SubagentAttemptPage
	attemptRequest      uint64
	attemptsLoading     bool
	attemptsErr         string
	attemptsRetryBefore int
	showProcess         bool
	process             *taskProcessPage
	inspectorFollow     bool
	liveOrder           []string
	newAttempt          bool
	seenTranscriptSeq   int
	showAttempts        bool
	attemptIndex        int
	committedResults    map[string]controlruntime.SubagentRecord
	processing          *processing.ProcessingItem
	liveMessage         *taskLiveMessage
	livePreview         *taskLivePreview
	eventSeq            uint64
	framePending        bool
}

type liveTaskTool struct {
	payload controlruntime.ToolPayload
	done    bool
}

type taskTickMsg struct{ generation uint64 }
type taskAnimationMsg struct{ generation uint64 }
type taskListMsg struct {
	generation, request uint64
	page                controlruntime.SubagentPage
	err                 error
}
type taskPromptMsg struct {
	generation, request uint64
	id, prompt          string
	err                 error
}

type taskHistoryViewport struct {
	index, line int
}

type taskHistoryMsg struct {
	viewport            *taskHistoryViewport
	following           bool
	prompt              string
	promptLoaded        bool
	generation, request uint64
	id                  string
	before              int
	page                controlruntime.SubagentHistoryPage
	err                 error
}
type taskAttemptsMsg struct {
	generation, request uint64
	id                  string
	before              int
	page                controlruntime.SubagentAttemptPage
	err                 error
}
type taskSubscribedMsg struct {
	generation, request uint64
	id                  string
	events              <-chan controlruntime.Event
	err                 error
}
type taskEventMsg struct {
	generation, request uint64
	id                  string
	event               controlruntime.Event
	ok                  bool
}

func (m *Model) toggleTasks() tea.Cmd {
	if m.tasks != nil && m.tasks.open {
		m.closeTasks()
		return nil
	}
	if !canObserveTasks(m.Runtime) {
		return nil
	}
	parent := ""
	if reader, ok := m.Runtime.(interface{ CurrentSessionID() string }); ok {
		parent = reader.CurrentSessionID()
	}
	if m.tasks == nil {
		m.tasks = &taskOverlay{}
	}
	s := m.tasks
	if s.sessionID != parent {
		generation := s.generation
		m.tasks = &taskOverlay{generation: generation, sessionID: parent}
		s = m.tasks
	}
	s.open = true
	s.background = ""
	s.renderedView = ""
	m.Messages.SetRenderingSuspended(true)
	s.generation++
	s.listLoading = false
	m.resizeTasks()
	cmds := []tea.Cmd{m.loadTasks(), taskTick(s.generation), taskAnimationTick(s.generation)}
	if s.detail != nil {
		cmds = append(cmds, m.subscribeTask(), m.loadTaskHistory(s.detail.currentCursor()), m.loadTaskAttempts())
		if out := s.detail.output; out != nil && out.resume {
			cmds = append(cmds, m.loadTaskOutput(out.requestedOffset))
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) closeTasks() {
	s := m.tasks
	if s == nil {
		return
	}
	s.open = false
	s.background = ""
	s.renderedView = ""
	m.Messages.SetRenderingSuspended(false)
	s.generation++
	s.listLoading = false
	if s.streamCancel != nil {
		s.streamCancel()
		s.streamCancel = nil
	}
	s.stream = nil
	s.subscribeRetryAt = time.Time{}
	if s.detail != nil {
		s.detail.loading = false
		s.detail.framePending = false
		if out := s.detail.output; out != nil {
			out.resume = out.loading
			out.loading = false
		}
	}
}

func taskAnimationTick(generation uint64) tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return taskAnimationMsg{generation} })
}

func taskTick(generation uint64) tea.Cmd {
	return tea.Tick(taskPollInterval, func(time.Time) tea.Msg { return taskTickMsg{generation} })
}

func (m *Model) loadTasks() tea.Cmd {
	s := m.tasks
	s.listDirty = false
	s.listQueriedAt = time.Now()
	s.listRequest++
	s.listLoading = true
	generation, request := s.generation, s.listRequest
	filter := controlruntime.SubagentFilter{AllSessions: s.allSessions, SessionID: s.sessionID, Query: s.query, Offset: s.listOffset, Limit: 1000}
	reader := m.Runtime.(controlruntime.SubagentReader)
	return func() tea.Msg {
		page, err := reader.Subagents(filter)
		return taskListMsg{generation, request, page, err}
	}
}

func (d *taskDetail) currentCursor() int {
	if len(d.cursors) == 0 {
		return 0
	}
	if len(d.cursors) == 1 && d.isLatestAttempt() {
		return taskAttemptCursor(d.attempt)
	}
	return d.cursors[len(d.cursors)-1]
}

func taskAttemptCursor(attempt *controlruntime.SubagentAttemptSummary) int {
	if attempt == nil {
		return 0
	}
	return attempt.TranscriptEnd + 1
}

func cloneTaskAttempt(attempt *controlruntime.SubagentAttemptSummary) *controlruntime.SubagentAttemptSummary {
	if attempt == nil {
		return nil
	}
	copy := *attempt
	return &copy
}

func (d *taskDetail) isLatestAttempt() bool {
	if d.attempt == nil {
		return d.info.LatestAttempt == nil || d.info.Status == "queued"
	}
	return d.info.LatestAttempt != nil && d.info.LatestAttempt.ID == d.attempt.ID
}

func (d *taskDetail) atLatestTail() bool {
	return d.isLatestAttempt() && len(d.cursors) <= 1
}

func (d *taskDetail) acceptsAttempt(id string) bool {
	if d.attempt == nil {
		return id == ""
	}
	return id != "" && id == d.attempt.ID
}

func (d *taskDetail) active() bool {
	status := d.detailStatus()
	return status == "running" || status == "queued" || strings.HasPrefix(status, "waiting_")
}

func (d *taskDetail) detailStatus() string {
	if d.attempt != nil {
		return d.attempt.Status
	}
	return d.info.Status
}

func (d *taskDetail) historyHasMore() bool {
	if !d.page.HasMore {
		return false
	}
	return d.attempt == nil || d.page.Before > d.attempt.TranscriptStart
}

func (m *Model) loadTaskAttempts() tea.Cmd {
	return m.loadTaskAttemptsBefore(0)
}

func (m *Model) loadTaskAttemptsBefore(before int) tea.Cmd {
	s, d := m.tasks, m.tasks.detail
	if d == nil || !d.info.Recorded {
		return nil
	}
	reader, ok := m.Runtime.(controlruntime.SubagentAttemptReader)
	if !ok {
		return nil
	}
	d.attemptRequest++
	d.attemptsLoading = true
	d.attemptsErr = ""
	generation, request, parent, id := s.generation, d.attemptRequest, m.taskParentSession(), d.id
	return func() tea.Msg {
		page, err := reader.SubagentAttempts(parent, id, before, 100)
		return taskAttemptsMsg{generation: generation, request: request, id: id, before: before, page: page, err: err}
	}
}

func (m *Model) loadTaskHistory(before int) tea.Cmd {
	s, d := m.tasks, m.tasks.detail
	if d == nil {
		return nil
	}
	if !d.info.Recorded || d.attempt == nil && d.info.Status == "queued" {
		reader, ok := m.Runtime.(interface {
			BackgroundTaskPrompt(string, string) (string, error)
		})
		if !ok || d.fullPromptLoaded {
			return nil
		}
		d.request++
		d.loading = true
		generation, request, parent, id := s.generation, d.request, m.taskParentSession(), d.id
		return func() tea.Msg {
			prompt, err := reader.BackgroundTaskPrompt(parent, id)
			return taskPromptMsg{generation: generation, request: request, id: id, prompt: prompt, err: err}
		}
	}
	if before == 0 {
		before = taskAttemptCursor(d.attempt)
	}
	if d.attempt != nil && d.attempt.TranscriptStart > d.attempt.TranscriptEnd {
		d.page = controlruntime.SubagentHistoryPage{SessionID: m.taskParentSession(), SubagentID: d.id}
		d.historyLoaded = true
		m.rebuildTaskDetail()
		return nil
	}
	d.request++
	d.loading = true
	if !d.showProcess && !d.historyLoaded {
		m.rebuildTaskDetail()
	}
	generation, request, parent, id := s.generation, d.request, m.taskParentSession(), d.id
	following := d.follow
	var viewport *taskHistoryViewport
	if len(d.page.Records) > 0 {
		index, line := d.list.ViewportAnchor()
		viewport = &taskHistoryViewport{index: index, line: line}
	}
	reader := m.Runtime.(controlruntime.SubagentReader)
	promptStart, promptEnd := 1, 0
	if d.attempt != nil {
		promptStart, promptEnd = d.attempt.TranscriptStart, d.attempt.TranscriptEnd
	}
	needPrompt := !d.fullPromptLoaded && promptStart > 0
	return func() tea.Msg {
		page, err := reader.SubagentHistory(parent, id, before, 100)
		reply := taskHistoryMsg{generation: generation, request: request, id: id, before: before, page: page, err: err, following: following, viewport: viewport}
		if err != nil || !needPrompt {
			return reply
		}
		end := page.Total
		if promptEnd > 0 {
			end = min(end, promptEnd)
		}
		for sequence := promptStart; sequence <= end; sequence++ {
			find := func(p controlruntime.SubagentHistoryPage) *controlruntime.SubagentRecord {
				for _, record := range p.Records {
					if record.Sequence == sequence && !record.ContextOnly {
						return &record
					}
				}
				return nil
			}
			record := find(page)
			if record == nil {
				// Resume can append interrupted tool outcomes before the task.
				// Read only that prefix, one indexed record at a time, without
				// loading older attempts or scanning the entire transcript.
				head, promptErr := reader.SubagentHistory(parent, id, sequence+1, 1)
				if promptErr != nil {
					reply.err = fmt.Errorf("读取完整任务提示词: %w", promptErr)
					return reply
				}
				record = find(head)
			}
			if record == nil {
				break
			}
			if record.Role == "user" && !record.RuntimeEvent {
				reply.prompt = strings.TrimPrefix(record.Content, "[Current delegated task]\n")
				reply.promptLoaded = true
				return reply
			}
			if record.Role != "tool" {
				break
			}
		}
		reply.err = fmt.Errorf("未找到本轮完整任务提示词（起始记录 %d）", promptStart)
		return reply
	}
}

func (m *Model) subscribeTask() tea.Cmd {
	s := m.tasks
	if s.streamCancel != nil {
		s.streamCancel()
	}
	s.stream = nil
	// Cross-session archives are read-only and refresh from their persisted
	// parent-scoped pages. The live event filter has only a task ID, which is
	// not sufficient to distinguish imported IDs reused by another session.
	if parent, ok := m.Runtime.(interface{ CurrentSessionID() string }); ok && m.taskParentSession() != parent.CurrentSessionID() {
		s.streamCancel = nil
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.streamCancel = cancel
	s.streamRequest++
	s.detail.framePending = false
	generation, id, request := s.generation, s.detail.id, s.streamRequest
	s.subscribeRetryAt = time.Now().Add(taskActiveResyncInterval)
	filter := controlruntime.EventFilter{SubAgentID: id, After: s.detail.eventSeq, Reliable: true, Types: []controlruntime.EventType{
		controlruntime.EventToolStarted, controlruntime.EventToolPreview, controlruntime.EventToolCompleted, controlruntime.EventToolBlocked,
		controlruntime.EventSubAgentUpdated, controlruntime.EventSubAgentEnded,
		controlruntime.EventSubAgentStarted, controlruntime.EventSubAgentOutput,
	}}
	return func() tea.Msg {
		if replay, ok := m.Runtime.(interface {
			ReplayEvents(context.Context, controlruntime.EventFilter) (<-chan controlruntime.Event, error)
		}); ok {
			events, err := replay.ReplayEvents(ctx, filter)
			return taskSubscribedMsg{generation: generation, request: request, id: id, events: events, err: err}
		}
		events, err := m.Runtime.Events(ctx, filter)
		return taskSubscribedMsg{generation: generation, request: request, id: id, events: events, err: err}
	}
}

func listenTaskEvent(events <-chan controlruntime.Event, generation, request uint64, id string) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		return taskEventMsg{generation: generation, request: request, id: id, event: ev, ok: ok}
	}
}

func (m *Model) handleTaskMessage(msg tea.Msg) (bool, tea.Cmd) {
	s := m.tasks
	if s == nil {
		return false, nil
	}
	if handled, cmd := m.handleTaskOutputMessage(msg); handled {
		return true, cmd
	}
	switch msg := msg.(type) {
	case taskAnimationMsg:
		if !s.open || msg.generation != s.generation {
			return true, nil
		}
		// The foreground clock may stop at any time; the overlay owns its own
		// generation-scoped clock. Reuse cached output when nothing animates.
		s.reuseView = true
		if d := s.detail; d != nil && d.processing != nil && d.active() && m.state != stateProcessing && m.state != stateConfirming && m.state != stateQuestioning {
			m.Spinner, _ = m.Spinner.Update(spinner.TickMsg{})
			d.processing.SetSpinnerView(m.Spinner.View())
			if d.list != nil {
				d.list.InvalidateItem(d.list.Len() - 1)
			}
			s.reuseView = false
		}
		return true, taskAnimationTick(s.generation)
	case taskFrameMsg:
		d := s.detail
		if !s.open || msg.generation != s.generation || msg.request != s.streamRequest || d == nil || msg.id != d.id {
			return true, nil
		}
		d.framePending = false
		if d.dirty && (!d.info.Recorded || d.historyLoaded) && (d.follow || d.inspector != nil || d.process != nil) && (d.atLatestTail() || d.process != nil) {
			m.rebuildTaskDetail()
			d.dirty = false
			return true, m.refreshTaskInspector()
		}
		return true, nil
	case taskTickMsg:
		if !s.open || msg.generation != s.generation {
			return true, nil
		}
		now := time.Now()
		var cmds []tea.Cmd
		if m.shouldRefreshTaskList(now) {
			cmds = append(cmds, m.loadTasks())
		}
		if d := s.detail; d != nil {
			if s.stream == nil && !s.subscribeRetryAt.IsZero() && !now.Before(s.subscribeRetryAt) {
				cmds = append(cmds, m.subscribeTask())
			}
			if d.dirty && (!d.info.Recorded || d.historyLoaded) && (d.follow || d.inspector != nil || d.process != nil) && (d.atLatestTail() || d.process != nil) {
				m.rebuildTaskDetail()
				cmds = append(cmds, m.refreshTaskInspector())
				d.dirty = false
			}
			if !d.loading && d.err == "" && d.info.Recorded && (!d.historyLoaded || ((d.follow || d.process != nil) && d.isLatestAttempt() && d.info.TranscriptSeq > d.page.Total)) {
				cmds = append(cmds, m.loadTaskHistory(d.currentCursor()))
			}
		}
		cmds = append(cmds, taskTick(s.generation))
		return true, tea.Batch(cmds...)
	case taskListMsg:
		if !s.open || msg.generation != s.generation || msg.request != s.listRequest {
			return true, nil
		}
		s.listLoading = false
		s.listLoaded = true
		if msg.err != nil {
			s.listError = msg.err.Error()
			return true, nil
		}
		s.listError = ""
		if !s.allSessions {
			s.sessionID = msg.page.SessionID
		}
		selectLast := s.appliedQuery == s.query && s.listOffset < s.appliedOffset
		resetOrder := s.appliedQuery != s.query || s.appliedOffset != s.listOffset
		s.items = mergeTaskRows(s.items, msg.page.Items, resetOrder)
		s.appliedQuery = s.query
		s.appliedOffset = s.listOffset
		s.listPage = msg.page
		found := false
		var detailCmd tea.Cmd
		for _, item := range s.items {
			if s.selectionKey(item) == s.selected {
				found = true
			}
			if s.detail != nil && sameTask(item, s.detail.info) {
				d := s.detail
				currentAttempt := item.LatestAttempt
				if item.Status == "queued" {
					currentAttempt = nil
				}
				followLatest := d.isLatestAttempt() && d.follow && d.process == nil && !d.showAttempts && d.inspector == nil
				if d.attempt == nil && !d.info.Recorded && currentAttempt != nil {
					d.attempt = cloneTaskAttempt(currentAttempt)
					d.cursors = []int{taskAttemptCursor(d.attempt)}
					d.historyLoaded = false
				}
				sameAttempt := d.attempt == nil && (currentAttempt == nil || item.Status == "queued") || d.attempt != nil && currentAttempt != nil && d.attempt.ID == currentAttempt.ID
				if sameAttempt {
					d.attempt = cloneTaskAttempt(currentAttempt)
				} else if currentAttempt != nil {
					d.newAttempt = true
				}
				d.info = item
				if !sameAttempt && currentAttempt != nil && followLatest {
					detailCmd = m.selectTaskAttempt(currentAttempt)
					continue
				}
				if d.isLatestAttempt() {
					d.newRecords = max(0, item.TranscriptSeq-d.seenTranscriptSeq)
				}
				if sameAttempt && (d.follow || d.process != nil) {
					m.rebuildTaskDetail()
				}
			}
		}
		if !found && len(s.items) > 0 {
			index := 0
			if selectLast {
				index = len(s.items) - 1
			}
			s.selected = s.selectionKey(s.items[index])
		}
		if s.listDirty {
			return true, tea.Batch(detailCmd, m.loadTasks())
		}
		return true, detailCmd
	case taskAttemptsMsg:
		d := s.detail
		if !s.open || msg.generation != s.generation || d == nil || msg.id != d.id || msg.request != d.attemptRequest {
			return true, nil
		}
		d.attemptsLoading = false
		if msg.err != nil {
			d.attemptsErr = msg.err.Error()
			d.attemptsRetryBefore = msg.before
			return true, nil
		}
		d.attemptsErr = ""
		d.attemptsRetryBefore = 0
		if msg.before > 0 {
			d.attempts.Attempts = append(msg.page.Attempts, d.attempts.Attempts...)
			d.attempts.Before, d.attempts.HasMore, d.attempts.Total = msg.page.Before, msg.page.HasMore, msg.page.Total
		} else {
			d.attempts = msg.page
		}
		return true, nil
	case taskPromptMsg:
		d := s.detail
		if !s.open || d == nil || msg.generation != s.generation || msg.request != d.request || msg.id != d.id {
			return true, nil
		}
		d.loading = false
		if msg.err != nil {
			d.err = msg.err.Error()
			return true, nil
		}
		d.err = ""
		d.fullPrompt, d.fullPromptLoaded = msg.prompt, msg.prompt != ""
		d.historyLoaded = true
		m.rebuildTaskDetail()
		return true, nil
	case taskHistoryMsg:
		d := s.detail
		if !s.open || msg.generation != s.generation || d == nil || msg.id != d.id || msg.request != d.request {
			return true, nil
		}
		d.loading = false
		d.historyLoaded = true
		if msg.err != nil {
			d.historyRetryBefore = msg.before
			d.err = msg.err.Error()
			return true, nil
		}
		d.err = ""
		d.historyRetryBefore = 0
		if msg.promptLoaded {
			d.fullPrompt, d.fullPromptLoaded = msg.prompt, true
		}
		index, line := d.list.ViewportAnchor()
		readerMoved := msg.viewport != nil && (index != msg.viewport.index || line != msg.viewport.line)
		tailRefresh := d.attempt != nil && d.isLatestAttempt() && len(d.cursors) == 1 && msg.before >= d.loadedCursor
		if d.process == nil && !d.follow && len(d.page.Records) > 0 && (readerMoved || msg.following || (msg.before == d.loadedCursor || tailRefresh) && msg.page.End > d.page.End) {
			d.newRecords = max(d.newRecords, msg.page.Total-d.seenTranscriptSeq)
			return true, nil
		}
		previousCursor := d.loadedCursor
		movingNewer := msg.before != previousCursor && (msg.before == 0 || previousCursor != 0 && msg.before > previousCursor)
		foundCursor := -1
		if d.isLatestAttempt() && len(d.cursors) == 1 && msg.before > d.cursors[0] {
			d.cursors[0] = msg.before
		}
		for i, cursor := range d.cursors {
			if cursor == msg.before {
				foundCursor = i
				break
			}
		}
		if msg.before == taskAttemptCursor(d.attempt) {
			d.cursors = []int{msg.before}
		} else if foundCursor >= 0 {
			d.cursors = d.cursors[:foundCursor+1]
		} else {
			d.cursors = append(d.cursors, msg.before)
		}
		hadPage := len(d.page.Records) > 0
		d.page = msg.page
		if d.liveMessage != nil && d.liveMessage.sequence <= msg.page.End {
			d.liveMessage = nil
		}
		if d.livePreview != nil && d.livePreview.sequence <= msg.page.End {
			d.livePreview = nil
		}
		d.loadedCursor = msg.before
		if d.isLatestAttempt() {
			if d.atLatestTail() {
				d.seenTranscriptSeq = max(d.seenTranscriptSeq, msg.page.Total)
			}
			d.newRecords = max(0, d.info.TranscriptSeq-d.seenTranscriptSeq)
		} else {
			d.newRecords = 0
		}
		knownCalls := make(map[string]bool)
		results := make(map[string]controlruntime.SubagentRecord)
		for _, record := range msg.page.Records {
			if record.ToolCallID != "" {
				results[record.ToolCallID] = record
			}
			for _, call := range record.ToolCalls {
				knownCalls[call.ID] = true
			}
			if record.ToolCallID != "" {
				delete(d.tools, record.ToolCallID)
			}
		}
		// Retain cross-page results only while their calls remain visible or live.
		for id, record := range d.committedResults {
			_, live := d.tools[id]
			if _, current := results[id]; !current && (knownCalls[id] || live) {
				results[id] = record
			}
		}
		d.committedResults = results
		for id, tool := range d.tools {
			if tool.done && !knownCalls[id] {
				delete(d.tools, id)
			}
		}
		liveOrder := d.liveOrder[:0]
		for _, id := range d.liveOrder {
			if _, ok := d.tools[id]; ok {
				liveOrder = append(liveOrder, id)
			}
		}
		d.liveOrder = liveOrder
		if d.process == nil && movingNewer && d.atLatestTail() {
			d.follow = true
		}
		m.rebuildTaskDetail()
		if d.process == nil && hadPage && previousCursor != msg.before {
			if d.follow || movingNewer {
				d.list.ScrollToBottom()
			} else {
				d.list.ScrollToTop()
			}
		}
		if p := d.process; p != nil && p.seekOldest {
			if len(p.calls) > 0 {
				p.selected = p.calls[0].entry.call.ID
				m.renderTaskProcessRows()
				p.list.ScrollToTop()
			}
			if d.historyHasMore() && !d.loading {
				return true, tea.Batch(m.refreshTaskInspector(), m.loadTaskHistory(d.page.Before))
			}
			p.seekOldest = false
		}
		return true, m.refreshTaskInspector()
	case taskSubscribedMsg:
		if !s.open || msg.generation != s.generation || msg.request != s.streamRequest || s.detail == nil || msg.id != s.detail.id {
			return true, nil
		}
		if msg.err != nil {
			s.stream = nil
			s.subscribeRetryAt = time.Now().Add(taskActiveResyncInterval)
			return true, nil
		} // committed-record polling remains available
		s.stream = msg.events
		s.subscribeRetryAt = time.Time{}
		return true, listenTaskEvent(msg.events, s.generation, msg.request, msg.id)
	case taskEventMsg:
		d := s.detail
		if !s.open || msg.generation != s.generation || msg.request != 0 && msg.request != s.streamRequest || d == nil || msg.id != d.id {
			return true, nil
		}
		if !msg.ok {
			s.stream = nil
			s.subscribeRetryAt = time.Now().Add(taskActiveResyncInterval)
			return true, nil
		} // the next subscription or committed read resynchronizes
		if msg.event.Sequence > 0 && msg.event.Sequence <= d.eventSeq {
			return true, listenTaskEvent(s.stream, s.generation, s.streamRequest, d.id)
		}
		if p, ok := msg.event.Payload.(controlruntime.SubAgentPayload); ok && msg.event.Type == controlruntime.EventSubAgentStarted && p.AttemptID != "" && d.attempt == nil && (!d.info.Recorded || d.info.Status == "queued") &&
			(d.info.LatestAttempt == nil || p.AttemptSeq > d.info.LatestAttempt.Seq && p.AttemptID != d.info.LatestAttempt.ID) {
			d.attempt = &controlruntime.SubagentAttemptSummary{ID: p.AttemptID, Seq: p.AttemptSeq, Status: p.Status, Prompt: p.Prompt}
			d.info.LatestAttempt = cloneTaskAttempt(d.attempt)
			d.request++
			d.loading = false
			d.historyLoaded = false
		}
		if p, ok := msg.event.Payload.(controlruntime.SubAgentOutput); ok {
			d.applyLiveOutput(msg.event, p)
		}
		if p, ok := msg.event.Payload.(controlruntime.ToolPayload); ok && p.CallID != "" && d.acceptsAttempt(p.AttemptID) {
			tool, exists := d.tools[p.CallID]
			if exists || len(d.tools) < 64 {
				if p.Args != "" {
					tool.payload.Args = p.Args
				}
				if p.ToolName != "" {
					tool.payload.ToolName = p.ToolName
				}
				tool.payload.CallID = p.CallID
				// Completion owns its execution receipts; later previews may omit
				// evidence and must not erase it or share the publisher's slice.
				if p.Evidence.Schema != 0 || len(p.Evidence.Executions) > 0 {
					tool.payload.Evidence = p.Evidence.Clone()
				}
				if msg.event.Type == controlruntime.EventToolPreview {
					tool.payload.Preview = p.Preview
				}
				if msg.event.Type == controlruntime.EventToolCompleted || msg.event.Type == controlruntime.EventToolBlocked {
					tool.payload.Output = p.Output
					tool.payload.IsError = p.IsError || msg.event.Type == controlruntime.EventToolBlocked
					tool.done = true
				}
				d.tools[p.CallID] = tool
				if !exists {
					d.liveOrder = append(d.liveOrder, p.CallID)
				}
				d.dirty = true
			}
		}
		refreshAttempts := msg.event.Type == controlruntime.EventSubAgentEnded && func() bool {
			p, ok := msg.event.Payload.(controlruntime.SubAgentPayload)
			return ok && p.AttemptID == ""
		}()
		if p, ok := msg.event.Payload.(controlruntime.SubAgentPayload); ok && d.acceptsAttempt(p.AttemptID) &&
			(msg.event.Time.IsZero() || d.info.UpdatedAt.IsZero() || msg.event.Time.After(d.info.UpdatedAt)) {
			if p.Status != "" {
				if d.attempt != nil {
					d.attempt.Status = p.Status
				}
				if d.isLatestAttempt() {
					d.info.Status = p.Status
				}
			}
			if p.Phase != "" && d.isLatestAttempt() {
				d.info.Phase = p.Phase
			}
			d.dirty = true
			refreshAttempts = msg.event.Type == controlruntime.EventSubAgentEnded
		}
		d.eventSeq = max(d.eventSeq, msg.event.Sequence)
		if d.process != nil {
			m.rebuildTaskProcess()
		}
		cmds := []tea.Cmd{listenTaskEvent(s.stream, s.generation, s.streamRequest, d.id)}
		cmds = append(cmds, m.scheduleTaskFrame())
		if refreshAttempts {
			cmds = append(cmds, m.loadTasks(), m.loadTaskAttempts())
		}
		return true, tea.Batch(cmds...)
	}
	return false, nil
}

func (m *Model) shouldRefreshTaskList(now time.Time) bool {
	s := m.tasks
	if s == nil || !s.open || s.listLoading {
		return false
	}
	elapsed := now.Sub(s.listQueriedAt)
	if !s.listLoaded || (s.listDirty && elapsed >= taskPollInterval) {
		return true
	}
	interval := taskIdleResyncInterval
	if m.state == stateProcessing || m.state == stateConfirming || m.state == stateQuestioning {
		interval = taskActiveResyncInterval
	}
	return elapsed >= interval
}

func (m *Model) openSelectedTask() tea.Cmd {
	s := m.tasks
	for _, item := range s.items {
		if s.selectionKey(item) != s.selected {
			continue
		}
		currentAttempt := item.LatestAttempt
		if item.Status == "queued" {
			currentAttempt = nil
		}
		restored := false
		if s.detail == nil || !sameTask(s.detail.info, item) {
			s.generation++
			if s.listLoading {
				s.listDirty = true
			}
			s.listLoading = false
			if s.savedDetail != nil && sameTask(s.savedDetail.info, item) {
				restored = true
				s.detail = s.savedDetail
				d := s.detail
				sameAttempt := d.attempt == nil && currentAttempt == nil || d.attempt != nil && currentAttempt != nil && d.attempt.ID == currentAttempt.ID
				if sameAttempt {
					d.attempt = cloneTaskAttempt(currentAttempt)
				}
				d.info = item
				if item.Status == "queued" {
					d.attempt = nil
					d.fullPrompt = ""
					d.fullPromptLoaded = false
					d.historyLoaded = false
				}
				s.savedDetail = nil
				if !sameAttempt && currentAttempt != nil {
					return tea.Batch(m.selectTaskAttempt(currentAttempt), m.loadTaskAttempts(), taskTick(s.generation), taskAnimationTick(s.generation))
				}
				if d.isLatestAttempt() {
					d.newRecords = max(0, item.TranscriptSeq-d.seenTranscriptSeq)
				}
			} else {
				s.detail = &taskDetail{id: item.ID, info: item, attempt: cloneTaskAttempt(currentAttempt), follow: true, cursors: []int{taskAttemptCursor(currentAttempt)}, seenTranscriptSeq: item.TranscriptSeq, tools: make(map[string]liveTaskTool), committedResults: make(map[string]controlruntime.SubagentRecord), list: components.NewList()}
			}
		}
		m.resizeTasks()
		historyCmd := m.loadTaskHistory(s.detail.currentCursor())
		if restored || s.detail.showProcess || historyCmd == nil {
			m.rebuildTaskDetail()
		}
		return tea.Batch(m.subscribeTask(), historyCmd, m.loadTaskAttempts(), taskTick(s.generation), taskAnimationTick(s.generation))
	}
	return nil
}

func (d *taskDetail) attemptList() []controlruntime.SubagentAttemptSummary {
	attempts := append([]controlruntime.SubagentAttemptSummary(nil), d.attempts.Attempts...)
	appendUnique := func(candidate *controlruntime.SubagentAttemptSummary) {
		if candidate == nil {
			return
		}
		for _, attempt := range attempts {
			if attempt.ID == candidate.ID {
				return
			}
		}
		attempts = append(attempts, *candidate)
	}
	appendUnique(d.info.LatestAttempt)
	appendUnique(d.attempt)
	sort.Slice(attempts, func(i, j int) bool { return attempts[i].Seq < attempts[j].Seq })
	return attempts
}

func (d *taskDetail) attemptDisplayList() []controlruntime.SubagentAttemptSummary {
	attempts := d.attemptList()
	for left, right := 0, len(attempts)-1; left < right; left, right = left+1, right-1 {
		attempts[left], attempts[right] = attempts[right], attempts[left]
	}
	return attempts
}

func (m *Model) toggleTaskAttempts() tea.Cmd {
	d := m.tasks.detail
	if d == nil {
		return nil
	}
	d.showAttempts = !d.showAttempts
	if !d.showAttempts {
		return nil
	}
	d.attemptIndex = 0
	if d.attempt != nil {
		for i, attempt := range d.attemptDisplayList() {
			if attempt.ID == d.attempt.ID {
				d.attemptIndex = i
				break
			}
		}
	}
	if len(d.attempts.Attempts) == 0 {
		return m.loadTaskAttempts()
	}
	return nil
}

func (m *Model) selectTaskAttempt(attempt *controlruntime.SubagentAttemptSummary) tea.Cmd {
	d := m.tasks.detail
	d.attempt = cloneTaskAttempt(attempt)
	d.fullPrompt, d.fullPromptLoaded = "", false
	d.showAttempts = false
	d.showProcess = false
	d.process = nil
	d.inspector = nil
	d.output = nil
	d.newAttempt = false
	d.follow = true
	d.newRecords = 0
	d.seenTranscriptSeq = attempt.TranscriptEnd
	d.page = controlruntime.SubagentHistoryPage{SessionID: m.taskParentSession(), SubagentID: d.id}
	cursor := taskAttemptCursor(d.attempt)
	d.cursors = []int{cursor}
	d.loadedCursor = cursor
	d.historyLoaded = false
	d.loading = false
	d.err = ""
	d.historyRetryBefore = 0
	d.tools = make(map[string]liveTaskTool)
	d.committedResults = make(map[string]controlruntime.SubagentRecord)
	d.liveMessage = nil
	d.livePreview = nil
	d.eventSeq = 0
	d.framePending = false
	d.liveOrder = nil
	d.entries = nil
	d.list.SetItems()
	d.request++
	// The previous subscription may have discarded this attempt's deltas while
	// another attempt was selected. Rebuild its live state from replay as well
	// as loading the committed transcript, and reject the old stream's replies.
	return tea.Batch(m.subscribeTask(), m.loadTaskHistory(cursor))
}

func (m *Model) stepTaskAttempt(delta int) tea.Cmd {
	d := m.tasks.detail
	if d == nil {
		return nil
	}
	if d.attempt == nil {
		if delta > 0 && d.info.LatestAttempt != nil {
			return m.selectTaskAttempt(d.info.LatestAttempt)
		}
		return nil
	}
	attempts := d.attemptList()
	index := -1
	for i := range attempts {
		if attempts[i].ID == d.attempt.ID {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}
	target := index + delta
	if target < 0 {
		if d.attempts.HasMore && !d.attemptsLoading {
			return m.loadTaskAttemptsBefore(d.attempts.Before)
		}
		return nil
	}
	if target >= len(attempts) {
		return nil
	}
	return m.selectTaskAttempt(&attempts[target])
}

func (m *Model) handleTaskKey(msg tea.KeyPressMsg) tea.Cmd {
	s := m.tasks
	if msg.String() == "ctrl+g" || msg.String() == "ctrl+c" {
		m.closeTasks()
		return nil
	}
	if s.searching {
		switch msg.String() {
		case "esc", "enter":
			s.searching = false
			return nil
		case "backspace":
			if len(s.query) > 0 {
				_, size := utf8.DecodeLastRuneInString(s.query)
				s.query = s.query[:len(s.query)-size]
			}
		default:
			if msg.Text != "" {
				if utf8.RuneCountInString(s.query) < 256 {
					s.query += msg.Text
				}
			} else {
				return nil
			}
		}
		s.listOffset = 0
		return m.loadTasks()
	}
	if d := s.detail; d != nil {
		if d.showAttempts {
			attempts := d.attemptDisplayList()
			if len(attempts) == 0 {
				d.attemptIndex = 0
			} else {
				d.attemptIndex = max(0, min(d.attemptIndex, len(attempts)-1))
			}
			switch msg.String() {
			case "esc", "/":
				d.showAttempts = false
			case "up":
				d.attemptIndex = max(0, d.attemptIndex-1)
			case "down":
				if d.attemptIndex == len(attempts)-1 && d.attempts.HasMore && !d.attemptsLoading {
					return m.loadTaskAttemptsBefore(d.attempts.Before)
				}
				d.attemptIndex = min(max(0, len(attempts)-1), d.attemptIndex+1)
			case "r":
				return m.loadTaskAttemptsBefore(d.attemptsRetryBefore)
			case "home":
				d.attemptIndex = 0
			case "end":
				d.attemptIndex = max(0, len(attempts)-1)
			case "enter":
				if len(attempts) > 0 {
					return m.selectTaskAttempt(&attempts[d.attemptIndex])
				}
			}
			return nil
		}
		if d.inspector != nil {
			switch msg.String() {
			case "up", "pgup", "home":
				d.inspectorFollow = false
			case "end":
				d.inspectorFollow = true
			}
			if msg.String() == "esc" {
				d.inspector = nil
				d.output = nil
				d.inspectedEntry = nil
				d.inspectedKey = ""
				return nil
			}
			if output := d.output; output != nil && !output.loading {
				if msg.String() == "end" && output.loaded && output.page.HasMore {
					return m.loadTaskOutput(max(0, output.page.Total-(64<<10)))
				}
				if msg.String() == "r" {
					if output.err != "" {
						return m.loadTaskOutput(output.requestedOffset)
					}
					return m.loadTaskOutput(output.page.Offset)
				}
				if (msg.String() == "down" || msg.String() == "pgdown") && d.inspector.AtBottom() && output.page.HasMore {
					return m.loadTaskOutput(output.page.NextOffset)
				}
				if (msg.String() == "up" || msg.String() == "pgup") && d.inspector.ScrollY() == 0 && len(output.offsets) > 1 {
					return m.loadTaskOutput(output.offsets[len(output.offsets)-2])
				}
			}
			scrollTaskList(d.inspector, msg.String())
			if msg.String() == "down" || msg.String() == "pgdown" {
				d.inspectorFollow = d.inspector.AtBottom()
			}
			return nil
		}
		if d.process != nil {
			return m.handleTaskProcessKey(msg.String())
		}
		switch msg.String() {
		case "/":
			return m.toggleTaskAttempts()
		case "esc":
			if s.streamCancel != nil {
				s.streamCancel()
				s.streamCancel = nil
			}
			s.stream = nil
			s.subscribeRetryAt = time.Time{}
			s.generation++
			s.listLoading = false
			s.listDirty = true
			d.loading = false
			d.request++
			s.savedDetail = d
			s.detail = nil
			return tea.Batch(taskTick(s.generation), taskAnimationTick(s.generation))
		case "enter":
			return m.inspectVisibleTaskTool()
		case "r":
			cursor := d.currentCursor()
			if d.err != "" {
				cursor = d.historyRetryBefore
			}
			return tea.Batch(m.loadTaskHistory(cursor), m.loadTaskAttempts())
		case "p":
			if d.showProcess {
				d.showProcess = false
				d.follow = true
				cursor := taskAttemptCursor(d.attempt)
				if d.currentCursor() != cursor {
					cmd := m.loadTaskHistory(cursor)
					m.rebuildTaskDetail()
					return cmd
				}
				m.rebuildTaskDetail()
				return nil
			}
			m.enterTaskProcess()
			return nil
		case "[":
			return m.stepTaskAttempt(-1)
		case "]":
			return m.stepTaskAttempt(1)
		case "end":
			d.list.ScrollToBottom()
			d.follow = true
			return tea.Batch(m.loadTaskHistory(taskAttemptCursor(d.attempt)), m.scheduleTaskFrame())
		case "home":
			d.follow = false
			if d.list.ScrollY() == 0 && d.historyHasMore() && !d.loading {
				return m.loadTaskHistory(d.page.Before)
			}
			d.list.ScrollToTop()
		case "up":
			return m.scrollTaskDetail(-1)
		case "pgup":
			return m.scrollTaskDetail(-max(1, d.list.Height()))
		case "down":
			return m.scrollTaskDetail(1)
		case "pgdown":
			return m.scrollTaskDetail(max(1, d.list.Height()))
		}
		return nil
	}
	switch msg.String() {
	case "a":
		s.allSessions = !s.allSessions
		s.listOffset = 0
		s.selected = ""
		s.items = nil
		return m.loadTasks()
	case "d":
		return m.requestTaskDelete()
	case "esc", "ctrl+c":
		m.closeTasks()
	case "/":
		s.searching = true
	case "r":
		return m.loadTasks()
	case "enter":
		return m.openSelectedTask()
	case "up", "down", "pgup", "pgdown", "home", "end":
		index := 0
		for i, item := range s.items {
			if s.selectionKey(item) == s.selected {
				index = i
				break
			}
		}
		switch msg.String() {
		case "up":
			index--
		case "down":
			index++
		case "pgup":
			index -= 10
		case "pgdown":
			index += 10
		case "home":
			index = 0
		case "end":
			index = len(s.items) - 1
		}
		if index >= len(s.items) && s.listPage.HasMore {
			s.listOffset = s.listPage.NextOffset
			return m.loadTasks()
		}
		if index < 0 && s.listOffset > 0 {
			s.listOffset = max(0, s.listOffset-1000)
			return m.loadTasks()
		}
		if len(s.items) > 0 {
			s.selected = s.selectionKey(s.items[max(0, min(index, len(s.items)-1))])
		}
	}
	return nil
}

// Scroll the detail with the same steps and follow transitions as the main view.
func (m *Model) scrollTaskDetail(lines int) tea.Cmd {
	d := m.tasks.detail
	if lines < 0 {
		d.follow = false
		if d.list.ScrollY() == 0 && d.historyHasMore() && !d.loading {
			return m.loadTaskHistory(d.page.Before)
		}
	} else if d.list.AtBottom() && len(d.cursors) > 1 && !d.loading {
		return m.loadTaskHistory(d.cursors[len(d.cursors)-2])
	}
	d.list.ScrollBy(lines)
	d.follow = d.list.AtBottom() && d.atLatestTail()
	if d.follow {
		return m.scheduleTaskFrame()
	}
	return nil
}

func scrollTaskList(list *components.List, key string) {
	switch key {
	case "up":
		list.ScrollBy(-1)
	case "down":
		list.ScrollBy(1)
	case "pgup":
		list.ScrollBy(-max(1, list.Height()))
	case "pgdown":
		list.ScrollBy(max(1, list.Height()))
	case "home":
		list.ScrollToTop()
	case "end":
		list.ScrollToBottom()
	}
}

func (m *Model) taskCommand(value string) (bool, tea.Cmd) {
	if strings.TrimSpace(value) != "/tasks" {
		return false, nil
	}
	if !canObserveTasks(m.Runtime) {
		return false, nil
	}
	return true, m.toggleTasks()
}

func taskStatus(status string) string {
	labels := map[string]string{"running": "● 运行中", "waiting_approval": "! 等待审批", "waiting_question": "? 等待回答", "completed": "✓ 完成", "partial": "◐ 部分完成", "failed": "× 失败", "interrupted": "■ 已中断", "unavailable": "× 记录不可用", "unknown": "· 已结束", "queued": "◷ 排队", "waiting_dependencies": "◷ 等待上游", "blocked": "■ 上游未成功", "not_started": "■ 未启动"}
	if label := labels[status]; label != "" {
		return label
	}
	return fmt.Sprintf("· %s", status)
}

func canObserveTasks(rt RuntimeClient) bool {
	if _, ok := rt.(controlruntime.SubagentReader); !ok {
		return false
	}
	if availability, ok := rt.(interface{ SubagentObservationAvailable() bool }); ok {
		return availability.SubagentObservationAvailable()
	}
	return true
}

func mergeTaskRows(previous, incoming []controlruntime.SubagentSummary, reset bool) []controlruntime.SubagentSummary {
	rank := func(status string) int {
		switch status {
		case "waiting_approval", "waiting_question", "failed", "unavailable", "blocked":
			return 0
		case "running":
			return 1
		case "queued", "waiting_dependencies":
			return 2
		}
		return 3
	}
	if len(previous) == 0 || reset {
		rows := append([]controlruntime.SubagentSummary(nil), incoming...)
		sort.SliceStable(rows, func(i, j int) bool { return rank(rows[i].Status) < rank(rows[j].Status) })
		return rows
	}
	byID := make(map[string]controlruntime.SubagentSummary, len(incoming))
	for _, item := range incoming {
		byID[taskIdentity(item)] = item
	}
	rows := make([]controlruntime.SubagentSummary, 0, len(incoming))
	for _, item := range previous {
		if updated, ok := byID[taskIdentity(item)]; ok {
			rows = append(rows, updated)
			delete(byID, taskIdentity(item))
		}
	}
	for _, item := range incoming {
		if _, ok := byID[taskIdentity(item)]; ok {
			rows = append(rows, item)
		}
	}
	return rows
}

func (m *Model) taskParentSession() string {
	if m.tasks.detail != nil && m.tasks.detail.info.SessionID != "" {
		return m.tasks.detail.info.SessionID
	}
	return m.tasks.sessionID
}
func (m *Model) requestTaskDelete() tea.Cmd {
	rt, ok := m.Runtime.(interface {
		DeleteSubagent(context.Context, string, string) error
	})
	if !ok {
		return nil
	}
	s := m.tasks
	if s == nil {
		return nil
	}
	generation := s.generation
	for _, item := range s.items {
		if s.selectionKey(item) != s.selected {
			continue
		}
		parent, id := item.SessionID, item.ID
		m.preConfirmState = m.state
		m.state = stateConfirming
		m.ConfirmBar.SetDestructive("删除任务", fmt.Sprintf("删除 %s 的任务记录和专属输出？工作区修改及 worktree 会保留。", id), "删除", func(confirmed bool) {
			if !confirmed || m.tasks != s || !s.open || s.generation != generation {
				return
			}
			if err := rt.DeleteSubagent(context.Background(), parent, id); err != nil {
				s.listError = err.Error()
				return
			}
			deletedIndex, removed := 0, 0
			kept := s.items[:0]
			for index, row := range s.items {
				if row.ID == id && row.SessionID == parent {
					deletedIndex = index
					removed++
					continue
				}
				kept = append(kept, row)
			}
			s.items = kept
			// Queries captured before deletion cannot restore the removed row.
			s.listRequest++
			s.listLoading = false
			s.listDirty = true
			s.listError = ""
			s.listPage.Total = max(0, s.listPage.Total-removed)
			if len(kept) == 0 && s.listOffset > 0 {
				s.listOffset = max(0, s.listOffset-1000)
			}
			selectedExists := false
			for _, row := range kept {
				if s.selectionKey(row) == s.selected {
					selectedExists = true
					break
				}
			}
			if !selectedExists {
				s.selected = ""
				if len(kept) > 0 {
					s.selected = s.selectionKey(kept[min(deletedIndex, len(kept)-1)])
				}
			}
			if s.savedDetail != nil && s.savedDetail.info.ID == id && s.savedDetail.info.SessionID == parent {
				s.savedDetail = nil
			}
			if s.detail != nil && s.detail.info.ID == id && s.detail.info.SessionID == parent {
				if s.streamCancel != nil {
					s.streamCancel()
					s.streamCancel = nil
				}
				s.stream = nil
				s.streamRequest++
				s.detail.request++
				s.detail = nil
			}
		})
		m.resizeMessages()
		return nil
	}
	return nil
}

func taskIdentity(item controlruntime.SubagentSummary) string {
	return item.SessionID + "\x00" + item.ID
}
func sameTask(left, right controlruntime.SubagentSummary) bool {
	return left.ID == right.ID && left.SessionID == right.SessionID
}
func (s *taskOverlay) selectionKey(item controlruntime.SubagentSummary) string {
	if s.allSessions {
		return taskIdentity(item)
	}
	return item.ID
}
