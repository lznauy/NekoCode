package tui

import (
	"context"
	"testing"

	controlruntime "nekocode/runtime"
)

// localFakeBot executes "/local" as a during-task-safe command and defers
// "/run" to the run path, tracking whether a run was ever started.
type localFakeBot struct {
	tickFakeBot
	runs int
}

func (b *localFakeBot) ExecuteLocalCommand(_ context.Context, input string) (string, controlruntime.LocalCommandResult) {
	switch input {
	case "/local":
		return "local ok", controlruntime.LocalCommandExecuted
	case "/run":
		return "", controlruntime.LocalCommandRequiresIdle
	default:
		return "", controlruntime.LocalCommandNotCommand
	}
}

func (b *localFakeBot) StartRun(ctx context.Context, input controlruntime.Input) (controlruntime.RunID, error) {
	b.runs++
	return b.tickFakeBot.StartRun(ctx, input)
}

func TestLocalCommandSkipsRunAndShowsOutput(t *testing.T) {
	bot := &localFakeBot{}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}

	if cmd := m.startChat("/local"); cmd != nil {
		t.Fatal("local command should not start the spinner/run")
	}
	if bot.runs != 0 {
		t.Fatalf("StartRun called %d times for a local command", bot.runs)
	}
}

func TestRunPathCommandRejectedWhileBusy(t *testing.T) {
	bot := &localFakeBot{}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.transitionTo(stateProcessing)

	if handled, _ := m.tryLocalCommand("/run"); !handled {
		t.Fatal("busy run-path command should be handled (rejected), not steered")
	}
	if bot.runs != 0 {
		t.Fatalf("StartRun called %d times", bot.runs)
	}

	// Same command goes through when idle: not handled by the local fork.
	m.transitionTo(stateReady)
	if handled, _ := m.tryLocalCommand("/run"); handled {
		t.Fatal("idle run-path command should fall through to StartRun")
	}
}
