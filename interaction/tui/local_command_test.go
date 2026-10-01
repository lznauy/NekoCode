package tui

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	controlruntime "nekocode/runtime"
	"nekocode/util/attachment"

	tea "charm.land/bubbletea/v2"
)

type failingStartBot struct{ localFakeBot }

func (b *failingStartBot) StartRun(context.Context, controlruntime.Input) (controlruntime.RunID, error) {
	return "", errors.New("model unavailable")
}

type failingSteerBot struct{ localFakeBot }

func (b *failingSteerBot) SteerRun(context.Context, controlruntime.RunID, controlruntime.Input) error {
	return errors.New("steer unavailable")
}

// localFakeBot executes "/local" as a during-task-safe command and defers
// "/run" to the run path, tracking whether a run was ever started.
type localFakeBot struct {
	tickFakeBot
	runs int
}

func (b *localFakeBot) ExecuteLocalCommand(_ context.Context, input string) (string, controlruntime.LocalCommandResult) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "", controlruntime.LocalCommandNotCommand
	}
	switch fields[0] {
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

	if cmd := m.startChat("/local", nil); cmd != nil {
		t.Fatal("local command should not start the spinner/run")
	}
	if bot.runs != 0 {
		t.Fatalf("StartRun called %d times for a local command", bot.runs)
	}
}

func TestLocalCommandPreservesUnsentImageDraft(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := attachment.SaveImage("session_1", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'})
	if err != nil {
		t.Fatal(err)
	}
	bot := &localFakeBot{}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.Input.SetValue("/local ")
	m.Input.SetCursorEnd()
	if !m.Input.AddImage(path) {
		t.Fatal("AddImage failed")
	}
	value := m.Input.Value()
	images := inputImageAttachments(m.Input.ImageAttachments())

	if cmd := m.startChat(value, images); cmd != nil {
		t.Fatal("local command should not start the spinner/run")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("local command deleted unsent draft image: %v", err)
	}
	if m.Input.Value() != value || len(m.Input.ImageAttachments()) != 1 {
		t.Fatalf("local command discarded image draft: value=%q images=%#v", m.Input.Value(), m.Input.ImageAttachments())
	}
	items := m.Messages.Items()
	if len(items) < 2 || !strings.Contains(items[len(items)-1].Render(100), "图片未发送") {
		t.Fatalf("local command did not explain preserved image draft: %#v", items)
	}
}

func TestProcessingLocalCommandPreservesUnsentImageDraft(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := attachment.SaveImage("session_1", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'})
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewModel(&localFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.transitionTo(stateProcessing)
	m.Input.SetValue("/local ")
	m.Input.SetCursorEnd()
	if !m.Input.AddImage(path) {
		t.Fatal("AddImage failed")
	}
	value := m.Input.Value()

	m.handleProcessingKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("processing local command deleted unsent draft image: %v", err)
	}
	if m.Input.Value() != value || len(m.Input.ImageAttachments()) != 1 {
		t.Fatalf("processing local command discarded image draft: value=%q images=%#v", m.Input.Value(), m.Input.ImageAttachments())
	}
}

func TestStartRunFailurePreservesTextAndImageDraft(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := attachment.SaveImage("session_1", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'})
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewModel(&failingStartBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.Input.SetValue("describe ")
	m.Input.SetCursorEnd()
	if !m.Input.AddImage(path) {
		t.Fatal("AddImage failed")
	}
	value := m.Input.Value()
	images := inputImageAttachments(m.Input.ImageAttachments())
	if cmd := m.startChat(value, images); cmd != nil {
		t.Fatal("failed run should not start spinner")
	}
	if m.Input.Value() != value || len(m.Input.ImageAttachments()) != 1 {
		t.Fatalf("draft was not preserved: value=%q images=%#v", m.Input.Value(), m.Input.ImageAttachments())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("draft image was deleted after start failure: %v", err)
	}
}

func TestHistorySubmissionFailureRestoresSavedImageDraft(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := attachment.SaveImage("session_1", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'})
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewModel(&failingStartBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.Input.SetHistory([]string{"older"})
	if !m.Input.AddImage(path) {
		t.Fatal("AddImage failed")
	}
	m.Input.HistoryUp()
	m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m.Input.HistoryDown()

	if got := m.Input.Value(); got != "[Image #1]" {
		t.Fatalf("restored draft value = %q", got)
	}
	images := m.Input.ImageAttachments()
	if len(images) != 1 || images[0].Path != path {
		t.Fatalf("restored draft images = %#v", images)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("saved draft image was deleted: %v", err)
	}
}

func TestAcceptedImageInputClearsDraftWithoutDeletingFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := attachment.SaveImage("session_1", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'})
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewModel(&localFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.Input.SetValue("describe ")
	m.Input.SetCursorEnd()
	if !m.Input.AddImage(path) {
		t.Fatal("AddImage failed")
	}
	m.handleRuntimeEvent(controlruntime.Event{
		Type:   controlruntime.EventInputAccepted,
		Source: controlruntime.SourceRef{Kind: "tui"},
		Payload: controlruntime.MessagePayload{
			Content: "describe [Image #1]", Source: controlruntime.SourceRef{Kind: "tui"},
			Images: []controlruntime.ImageAttachment{{Label: "[Image #1]", Path: path}},
		},
	})
	if m.Input.HasContent() || len(m.Input.ImageAttachments()) != 0 {
		t.Fatalf("accepted image draft remained: %q %#v", m.Input.Value(), m.Input.ImageAttachments())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("accepted image file was deleted: %v", err)
	}
}

func TestSteerRunFailurePreservesTextDraft(t *testing.T) {
	m, err := NewModel(&failingSteerBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.transitionTo(stateProcessing)
	m.processingPhase = PhaseWaiting
	m.Input.SetValue("follow up")
	m.handleProcessingKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if got := m.Input.Value(); got != "follow up" {
		t.Fatalf("steer failure discarded draft: %q", got)
	}
	if m.processingPhase != PhaseWaiting {
		t.Fatalf("steer failure left phase at %q", m.processingPhase)
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
