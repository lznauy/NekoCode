// model.go — Model 结构体 + 初始化 + 状态切换。
package tui

import (
	"fmt"
	"strings"
	"time"

	"nekocode/tui/components"
	"nekocode/tui/styles"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"nekocode/common")

type Model struct {
	Bot      BotInterface
	Header   *components.Header
	Messages *components.Messages
	Input    *components.Input
	Splash   *components.Splash
	Spinner  spinner.Model
	Width    int
	Height   int
	Ready    bool

	state           chatState
	processingStart time.Time
	processingPhase string
	activeSkill     string // skill activated this turn, shown in status bar
	Suggestions     *components.Suggestions
	ConfirmBar      *components.ConfirmBar
	Scrollbar       *components.Scrollbar
	confirmCh       chan common.ConfirmRequest
}

const version = "0.2.0"

func NewModel(b BotInterface) *Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sty := styles.DefaultStyles()

	m := &Model{
		Bot:         b,
		Header:      components.NewHeader(80, b.Provider(), b.Model(), version),
		Messages:    components.NewMessages(80, 14, &sty),
		Input:       components.NewInput(80),
		Splash:      components.NewSplash(80, 24, version),
		Spinner:     sp,
		Suggestions: components.NewSuggestions(&sty),
		ConfirmBar:  components.NewConfirmBar(&sty),
		Scrollbar:   components.NewScrollbar(&sty),
		Width:       80,
		Height:      24,
		state:       stateReady,
		confirmCh:   make(chan common.ConfirmRequest),
	}

	b.Configure(
		func(req common.ConfirmRequest) bool {
			m.confirmCh <- req
			return <-req.Response
		},
		func(phase string) {
			m.setPhase(phase)
		},
		func(items []common.TodoItem) {
			m.Messages.SetTodos(todoItemsText(items))
		},
	)

	return m
}

func (m *Model) Init() tea.Cmd {
	return m.Input.Init()
}

func (m *Model) resizeMessages() {
	extra := 0
	if m.state == stateConfirming {
		extra += m.ConfirmBar.Height(m.Width)
	}
	if m.Suggestions.Visible() {
		extra += m.Suggestions.Height()
	}
	m.Messages.SetSize(m.Width-1, m.Height-m.Header.Height()-m.Input.Height()-contentMarginV-extra)
}

func (m *Model) transitionTo(state chatState) {
	m.state = state
	switch state {
	case stateReady:
		m.setPhase(PhaseReady)
		m.Messages.SetProcessing(false)
		m.Input.SetSending(false)
		m.ConfirmBar.Clear()
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

func listenConfirm(ch <-chan common.ConfirmRequest) tea.Cmd {
	return func() tea.Msg {
		req, ok := <-ch
		if !ok {
			return nil
		}
		return confirmMsg{req: req}
	}
}

// Processing phases displayed in the status line during agent execution.
const (
	phaseSteer     = "Processing new input..."
	PhaseReady     = common.PhaseReady
	PhaseWaiting   = common.PhaseWaiting
	PhaseThinking  = common.PhaseThinking
	PhaseReasoning = common.PhaseReasoning
	PhaseRunning   = common.PhaseRunning
)

func (m *Model) setPhase(p string) {
	if m.processingPhase == phaseSteer && p == PhaseWaiting {
		return
	}
	m.processingPhase = p
}

func todoItemsText(items []common.TodoItem) string {
	if len(items) == 0 {
		return ""
	}
	done := 0
	for _, it := range items {
		if it.Status == "completed" {
			done++
		}
	}
	if done == len(items) {
		// All complete — show a compact summary instead of hiding.
		return fmt.Sprintf("✓ All %d tasks complete", done)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Tasks %d/%d", done, len(items)))
	for _, it := range items {
		icon := "·" // · pending
		switch it.Status {
		case "in_progress":
			icon = "▸" // ▸ in progress
		case "completed":
			icon = "✓" // ✓ completed
		}
		b.WriteString(fmt.Sprintf("\n%s %s", icon, it.Content))
	}
	return b.String()
}
