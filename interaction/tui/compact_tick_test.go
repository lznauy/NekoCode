package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	controlruntime "nekocode/runtime"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

type tickFakeBot struct {
	mu        sync.Mutex
	submitted []string
}

func (b *tickFakeBot) StartRun(_ context.Context, input controlruntime.Input) (controlruntime.RunID, error) {
	b.mu.Lock()
	b.submitted = append(b.submitted, input.Text)
	b.mu.Unlock()
	return "", nil
}
func (*tickFakeBot) SteerRun(context.Context, controlruntime.RunID, controlruntime.Input) error {
	return nil
}
func (*tickFakeBot) ExecuteLocalCommand(context.Context, string) (string, controlruntime.LocalCommandResult) {
	return "", controlruntime.LocalCommandNotCommand
}
func (*tickFakeBot) CancelRun(context.Context, controlruntime.RunID) error { return nil }
func (*tickFakeBot) DecideApproval(context.Context, string, controlruntime.ApprovalDecision) error {
	return nil
}
func (*tickFakeBot) AnswerQuestion(context.Context, string, controlruntime.QuestionReply) error {
	return nil
}
func (*tickFakeBot) Events(context.Context, controlruntime.EventFilter) (<-chan controlruntime.Event, error) {
	return make(chan controlruntime.Event), nil
}
func (*tickFakeBot) CommandMenu(context.Context, string) (controlruntime.CommandMenu, bool) {
	return controlruntime.CommandMenu{}, false
}
func (*tickFakeBot) Close() error { return nil }
func (*tickFakeBot) CurrentModel() controlruntime.ModelSelection {
	return controlruntime.ModelSelection{Provider: "test", Model: "test"}
}
func (*tickFakeBot) PermissionMode() string { return "manual" }
func (*tickFakeBot) ContextSnapshot() controlruntime.ContextSnapshot {
	return controlruntime.ContextSnapshot{}
}
func (*tickFakeBot) WorkspaceChanges() controlruntime.WorkspaceChanges {
	return controlruntime.WorkspaceChanges{}
}
func (*tickFakeBot) SwitchModel(string) (controlruntime.ModelSelection, error) {
	return controlruntime.ModelSelection{}, nil
}
func (*tickFakeBot) SessionMessages() []controlruntime.DisplayMessage {
	return nil
}
func (*tickFakeBot) CurrentSessionID() string                   { return "" }
func (*tickFakeBot) ResumeSession(string) error                 { return nil }
func (*tickFakeBot) ListSessions() []controlruntime.SessionMeta { return nil }
func (*tickFakeBot) NewSession() (controlruntime.SessionMeta, error) {
	return controlruntime.SessionMeta{}, nil
}
func (*tickFakeBot) DeleteSession(string) error { return nil }

func (b *tickFakeBot) submittedInputs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.submitted...)
}

type blockingMetricsBot struct {
	tickFakeBot
	metricsCalled chan struct{}
}

func (b *blockingMetricsBot) Metrics() controlruntime.MetricsSnapshot {
	close(b.metricsCalled)
	select {}
}

func TestCompactProcessingTickUpdatesView(t *testing.T) {
	bot := &tickFakeBot{}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	model, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = model.(*Model)

	cmd := m.startChat("/compact")
	if cmd == nil {
		t.Fatal("startChat should tick while compacting")
	}
	if got := bot.submittedInputs(); len(got) != 1 || got[0] != "/compact" {
		t.Fatalf("submitted inputs = %#v, want /compact", got)
	}

	before := fmt.Sprint(m.View())
	if !strings.Contains(before, "Compacting context...") {
		t.Fatalf("compact view missing status: %q", before)
	}

	model, _ = m.Update(spinner.TickMsg{})
	m = model.(*Model)
	afterOne := fmt.Sprint(m.View())
	model, _ = m.Update(spinner.TickMsg{})
	m = model.(*Model)
	afterTwo := fmt.Sprint(m.View())

	if before == afterOne {
		t.Fatalf("first processing tick did not change view")
	}
	if afterOne == afterTwo {
		t.Fatalf("second processing tick did not change view")
	}
}

func TestCompactProcessingTickDoesNotReadMetrics(t *testing.T) {
	bot := &blockingMetricsBot{metricsCalled: make(chan struct{})}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	model, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = model.(*Model)

	m.transitionTo(stateProcessing)
	m.setPhase(phaseCompacting)

	done := make(chan struct{})
	go func() {
		_ = m.handleSpinnerTick(spinner.TickMsg{})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("compact processing tick blocked")
	}

	select {
	case <-bot.metricsCalled:
		t.Fatal("compact processing tick should not call runtime metrics")
	default:
	}
}
