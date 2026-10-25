// model.go — Model 结构体 + 初始化 + 状态切换。
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"nekocode/interaction/tui/components"
	"nekocode/interaction/tui/components/message"
	"nekocode/interaction/tui/styles"
	controlruntime "nekocode/runtime"
	"nekocode/util/attachment"
	"nekocode/util/version"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

type RuntimeClient interface {
	controlruntime.Interaction
	SteerRun(context.Context, controlruntime.RunID, controlruntime.Input) error
	ExecuteLocalCommand(context.Context, string) (string, controlruntime.LocalCommandResult)
	CurrentModel() controlruntime.ModelSelection
	PermissionMode() string
	ContextSnapshot() controlruntime.ContextSnapshot
	WorkspaceChanges() controlruntime.WorkspaceChanges
	SessionMessages() []controlruntime.DisplayMessage
	Close() error
}

type sessionDeleter interface {
	DeleteSession(string) error
}

type Model struct {
	Runtime        RuntimeClient
	Header         *components.Header
	Messages       *components.Messages
	Input          *components.Input
	Splash         *components.Splash
	Spinner        spinner.Model
	Width          int
	Height         int
	Ready          bool
	terminalWidth  int
	terminalHeight int

	state                 chatState
	preConfirmState       chatState
	processingStart       time.Time
	processingPhase       string
	Suggestions           *components.Suggestions
	ConfirmBar            *components.ConfirmBar
	QuestionBar           *components.QuestionBar
	Scrollbar             *components.Scrollbar
	runtimeEvents         <-chan controlruntime.Event
	metrics               controlruntime.MetricsSnapshot
	commandMenuBack       []string
	sessionLoadGeneration uint64
	sessionLoading        bool
	sessionLoadStarted    time.Time
	sessionLoadLabel      string
	sessionLoadWaitForRun bool
	sessionLoadPhase      string
	sessionLoadDone       *doneMsg
	sessionLoadNotices    []message.ChatMessage
	sessionHistoryCursors []int
	sessionHistoryPage    int
	sessionHistoryBefore  int
	sessionHistoryPaging  bool
	sessionHistoryDirty   bool
	sessionPendingChat    *pendingSessionChat
	tasks                 *taskOverlay
	taskStates            map[string]string // current parent run only
	taskSummarySyncedAt   time.Time
}

var displayVersion = version.Version

func NewModel(rt RuntimeClient) (*Model, error) {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sty := styles.DefaultStyles()

	filter := controlruntime.EventFilter{}
	if canObserveTasks(rt) {
		filter.ExcludeSubagentDetails = true
	}
	events, err := rt.Events(context.Background(), filter)
	if err != nil {
		return nil, fmt.Errorf("subscribe runtime events: %w", err)
	}
	m := &Model{
		Runtime:       rt,
		Header:        components.NewHeader(80, displayVersion),
		Messages:      components.NewMessages(80, 14, &sty),
		Input:         components.NewInput(80),
		Splash:        components.NewSplash(80, 24, displayVersion),
		Spinner:       sp,
		Suggestions:   components.NewSuggestions(&sty),
		ConfirmBar:    components.NewConfirmBar(&sty),
		QuestionBar:   components.NewQuestionBar(&sty),
		Scrollbar:     components.NewScrollbar(&sty),
		Width:         80,
		Height:        24,
		state:         stateReady,
		runtimeEvents: events,
	}
	m.Input.SetTaskObserver(canObserveTasks(rt))
	m.Input.SetHistory(loadInputHistory())
	m.Input.SetImageCleanup(func(path string) { _ = attachment.DeleteImage(path) })
	m.refreshRuntimeStatus()

	return m, nil
}

type runtimeViewStatus struct {
	selection  controlruntime.ModelSelection
	context    controlruntime.ContextSnapshot
	permission string
}

func readRuntimeViewStatus(rt RuntimeClient) runtimeViewStatus {
	return runtimeViewStatus{selection: rt.CurrentModel(), context: rt.ContextSnapshot(), permission: rt.PermissionMode()}
}

func (m *Model) refreshRuntimeStatus() { m.applyRuntimeViewStatus(readRuntimeViewStatus(m.Runtime)) }

func (m *Model) applyRuntimeViewStatus(status runtimeViewStatus) {
	m.Header.SetContext(status.context.Used, status.context.Budget, status.context.CompactionThreshold,
		status.context.CacheHitTokens, status.context.CacheMissTokens)
	label := status.selection.Model
	if status.selection.Provider != "" && status.selection.Model != "" {
		label = status.selection.Provider + "/" + status.selection.Model
	} else if status.selection.Provider != "" {
		label = status.selection.Provider
	}
	m.Input.SetModel(label)
	m.Input.SetReasoningEffort(status.selection.ReasoningEffort)
	m.Input.SetPermissionMode(status.permission)
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.Input.Init(), listenRuntimeEvent(m.runtimeEvents), loadWorkspaceChanges(m.Runtime))
}

func loadWorkspaceChanges(rt RuntimeClient) tea.Cmd {
	return func() tea.Msg { return workspaceChangesMsg(rt.WorkspaceChanges()) }
}

func (m *Model) resizeMessages() {
	follow := m.Messages.Follow
	extra := 0
	if m.state == stateConfirming {
		extra += m.ConfirmBar.Height(m.Width, m.Height)
	}
	if m.state == stateQuestioning {
		extra += m.QuestionBar.Height(m.Width, m.Height)
	}
	if m.Suggestions.Visible() {
		extra += m.Suggestions.Height()
	}
	msgHeight := max(0, m.Height-m.Header.Height()-m.Input.Height()-contentMarginV-extra)
	// Horizontal scrollbar occupies 1 row above the header when content
	// overflows the viewport; reserve it so the message area shrinks.
	totalHeight, _, _ := m.Messages.ScrollbarMetrics()
	if totalHeight > msgHeight && msgHeight > 0 {
		msgHeight--
	}
	width := m.Width
	m.Messages.SetSize(width, msgHeight)
	if follow {
		m.Messages.GotoBottom()
	}
}

func (m *Model) transitionTo(state chatState) {
	m.state = state
	switch state {
	case stateReady:
		m.setPhase(PhaseReady)
		m.Messages.SetProcessing(false)
		m.Input.SetSending(false)
		m.ConfirmBar.Clear()
		m.QuestionBar.Clear()
	case stateProcessing:
		m.processingStart = time.Now()
		m.setPhase(PhaseWaiting)
		m.Messages.SetProcessingStatus(PhaseWaiting)

		m.Messages.SetProcessing(true)
		m.Input.SetSending(true)
	case stateConfirming:
	}
	m.resizeMessages()
}

func listenRuntimeEvent(ch <-chan controlruntime.Event) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return runtimeEventMsg{event: ev}
	}
}

// Processing phases displayed in the status line during agent execution.
const (
	phaseSteer          = "Processing new input..."
	phaseCompacting     = "Compacting context..."
	phaseSessionLoading = "正在加载会话…"
	PhaseReady          = "Ready"
	PhaseWaiting        = "Waiting"
)

func (m *Model) setPhase(p string) {
	if m.processingPhase == phaseSteer && p == PhaseWaiting {
		return
	}
	m.processingPhase = p
}

func todoItemsText(items []controlruntime.TodoItem) string {
	if len(items) == 0 {
		return ""
	}
	done := countCompleted(items)
	if done == len(items) {
		return fmt.Sprintf("✓ All %d tasks complete", done)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Tasks %d/%d", done, len(items))
	for _, it := range items {
		fmt.Fprintf(&b, "\n%s %s", todoStatusIcon(it.Status), it.Content)
	}
	return b.String()
}

func countCompleted(items []controlruntime.TodoItem) int {
	n := 0
	for _, it := range items {
		if it.Status == "completed" {
			n++
		}
	}
	return n
}

func todoStatusIcon(status string) string {
	switch status {
	case "in_progress":
		return "▸"
	case "completed":
		return "✓"
	default:
		return "·"
	}
}
