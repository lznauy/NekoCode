package tui

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"nekocode/interaction/tui/components/message"
	"nekocode/interaction/tui/components/processing"
	"nekocode/protocol"
	controlruntime "nekocode/runtime"
	"nekocode/util/attachment"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestCompactionStaysInsideStatusFrameWithoutDuplicateResponse(t *testing.T) {
	m, err := NewModel(&statusFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	e := controlruntime.CompactionPayload{ID: "compact", Status: "started", Trigger: "manual", BeforeMessages: 1415, BeforeTokens: 295525}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventCompaction, Payload: e})
	e.Status, e.Delta = "delta", "streamed summary content"
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventCompaction, Payload: e})
	items := m.Messages.Items()
	if len(items) != 1 {
		t.Fatalf("compaction escaped status frame: %d items", len(items))
	}
	if _, ok := items[0].(*processing.ProcessingItem); !ok {
		t.Fatalf("expected processing frame, got %T", items[0])
	}
	view := ansi.Strip(items[0].Render(100))
	if !strings.Contains(view, "streamed summary content") || !strings.Contains(view, "│") {
		t.Fatalf("missing framed stream: %s", view)
	}
	e.Status, e.Summary, e.Delta, e.AfterMessages, e.AfterTokens = "completed", "final summary content", "", 36, 17553
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventCompaction, Payload: e})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSystemMessage, Payload: controlruntime.MessagePayload{Content: "Compacted: 1415 messages, ~295525 → ~17553 tokens"}})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunDone})
	items = m.Messages.Items()
	if len(items) != 1 {
		t.Fatalf("duplicate completion output: %d items", len(items))
	}
	view = ansi.Strip(items[0].Render(100))
	t.Logf("settled compaction entry:\n%s", view)
	if !strings.Contains(view, "Compacted") || strings.Contains(view, "│") || strings.Contains(view, "final summary content") {
		t.Fatalf("expected flat settled entry: %s", view)
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunDone})
	if len(m.Messages.Items()) != 1 {
		t.Fatal("next run lost compaction history")
	}
}

func TestCompactionNoopStillShowsCommandFeedback(t *testing.T) {
	m, err := NewModel(&statusFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSystemMessage, Payload: controlruntime.MessagePayload{Content: "Conversation too short, nothing to compact."}})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunDone})
	items := m.Messages.Items()
	if len(items) != 1 || !strings.Contains(ansi.Strip(items[0].Render(100)), "nothing to compact") {
		t.Fatalf("lost no-op command feedback: %+v", items)
	}
}

func TestQuestionCustomAnswerAcceptsPasteAndMultiRuneText(t *testing.T) {
	m, err := NewModel(&statusFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	var reply controlruntime.QuestionReply
	m.QuestionBar.SetRequest(&controlruntime.QuestionRequest{Questions: []controlruntime.QuestionItem{{
		Question: "q", Options: []controlruntime.QuestionOption{{Label: "option"}}, Custom: true,
	}}}, func(got controlruntime.QuestionReply) { reply = got })
	m.QuestionBar.Move(1)
	m.state = stateQuestioning

	m.Update(tea.PasteMsg{Content: "粘贴\r\n内容"})
	m.handleQuestionKey(tea.KeyPressMsg(tea.Key{Code: 'e', Text: "enter"}))
	m.handleQuestionKey(tea.KeyPressMsg(tea.Key{Code: '补', Text: "补充"}))
	m.QuestionBar.Submit()

	if len(reply.Answers) != 1 || len(reply.Answers[0]) != 1 || reply.Answers[0][0] != "粘贴 内容enter补充" {
		t.Fatalf("custom answer = %#v", reply.Answers)
	}
	if m.Input.HasContent() {
		t.Fatalf("question paste leaked into main input: %q", m.Input.Value())
	}
}

func TestAutoCompactionCancellationStaysInFrame(t *testing.T) {
	m, err := NewModel(&statusFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	e := controlruntime.CompactionPayload{ID: "auto", Status: "delta", Trigger: "auto", Delta: "partial summary"}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventCompaction, Payload: e})
	if !strings.Contains(ansi.Strip(m.Messages.Items()[0].Render(100)), "Compacting context...") {
		t.Fatal("automatic compaction missing active status")
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunCancelled})
	view := ansi.Strip(m.Messages.Items()[0].Render(100))
	if !strings.Contains(view, "partial summary") || !strings.Contains(view, "Compaction interrupted") || strings.Contains(view, "│") {
		t.Fatalf("cancelled summary lost: %s", view)
	}
}

type commandFakeBot struct {
	tickFakeBot
	commands []string
	menus    map[string]controlruntime.CommandMenu
}

type statusFakeBot struct {
	tickFakeBot
	selection controlruntime.ModelSelection
}

type sessionDeleteFakeBot struct {
	commandFakeBot
	deleted   []string
	deleteErr error
}

func (b *sessionDeleteFakeBot) DeleteSession(id string) error {
	b.deleted = append(b.deleted, id)
	return b.deleteErr
}

type committedSessionDeleteError struct{ error }

func (committedSessionDeleteError) MutationCommitted() bool { return true }

type imageCommandFakeBot struct {
	commandFakeBot
	inputs  []controlruntime.Input
	steered []controlruntime.Input
}

func (b *imageCommandFakeBot) StartRun(_ context.Context, input controlruntime.Input) (controlruntime.RunID, error) {
	b.inputs = append(b.inputs, input)
	return "", nil
}

func (b *imageCommandFakeBot) SteerRun(_ context.Context, _ controlruntime.RunID, input controlruntime.Input) error {
	b.steered = append(b.steered, input)
	return nil
}

type localMenuFakeBot struct{ localFakeBot }

func (*localMenuFakeBot) CommandMenu(_ context.Context, input string) (controlruntime.CommandMenu, bool) {
	if input != "/local" {
		return controlruntime.CommandMenu{}, false
	}
	return controlruntime.CommandMenu{Items: []controlruntime.CommandMenuItem{{
		Value: "/local detail", Label: "detail", Submit: true,
	}}}, true
}

func (b *statusFakeBot) CurrentModel() controlruntime.ModelSelection { return b.selection }

func TestSystemEventRefreshesInputModelFooter(t *testing.T) {
	bot := &statusFakeBot{selection: controlruntime.ModelSelection{Provider: "openai", Model: "old", ReasoningEffort: "low"}}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	bot.selection.Model = "new"
	bot.selection.ReasoningEffort = "high"
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSystemMessage})
	footer := ansi.Strip(m.Input.View())
	if !strings.Contains(footer, "openai/new") || !strings.Contains(footer, "Effort: high") || strings.Contains(footer, "openai/old") || strings.Contains(footer, "Effort: low") {
		t.Fatalf("model footer was not refreshed:\n%s", footer)
	}
}

func TestRunDoneAttachesCallUsageToAssistantTurn(t *testing.T) {
	bot := &statusFakeBot{}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventMetricsUpdated, Payload: controlruntime.MetricsSnapshot{
		Duration: "2.1s", TurnTotal: 22_597, TurnInput: 21_865, TurnCached: 21_200,
		TurnNew: 665, TurnOutput: 732, TurnReasoning: 283, TurnCacheReported: true,
	}})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunDone, Payload: controlruntime.RunResult{Output: "Done"}})

	items := m.Messages.Items()
	assistant, ok := items[len(items)-1].(*message.AssistantMessageItem)
	if !ok {
		t.Fatalf("last item = %T, want assistant message", items[len(items)-1])
	}
	clean := ansi.Strip(assistant.Render(180))
	for _, want := range []string{"↳ 2.1s", "总计 22.6k tok", "输入 21.9k", "缓存 21.2k", "未缓存 665", "输出 732", "推理 283", "本次命中 96.96%"} {
		if !strings.Contains(clean, want) {
			t.Fatalf("assistant turn telemetry missing %q:\n%s", want, clean)
		}
	}
}

func TestRunFailureKeepsUsageAsStandaloneMetaLine(t *testing.T) {
	bot := &statusFakeBot{}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventMetricsUpdated, Payload: controlruntime.MetricsSnapshot{
		TurnInput: 1000, TurnOutput: 12,
	}})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunFailed, Payload: controlruntime.RunResult{Error: "provider failed"}})

	items := m.Messages.Items()
	telemetry, ok := items[len(items)-1].(*message.TelemetryMessageItem)
	if !ok {
		t.Fatalf("last item = %T, want telemetry message", items[len(items)-1])
	}
	clean := ansi.Strip(telemetry.Render(140))
	if !strings.Contains(clean, "输入 1.0k") || !strings.Contains(clean, "缓存 —") || !strings.Contains(clean, "输出 12") {
		t.Fatalf("failure telemetry missing usage:\n%s", clean)
	}
}

func (b *commandFakeBot) CommandMenu(_ context.Context, input string) (controlruntime.CommandMenu, bool) {
	if input == "/" {
		items := make([]controlruntime.CommandMenuItem, 0, len(b.commands))
		for _, value := range b.commands {
			items = append(items, controlruntime.CommandMenuItem{Value: value, Label: value})
		}
		return controlruntime.CommandMenu{Title: "Commands", Items: items}, len(items) > 0
	}
	menu, ok := b.menus[input]
	return menu, ok
}

func TestEnterCompletesSelectedCommandBeforeSubmitting(t *testing.T) {
	bot := &commandFakeBot{commands: []string{"/help", "/status"}}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}

	m.Input.SetValue("/")
	m.refreshSuggestions()
	if !m.Suggestions.Visible() {
		t.Fatal("command suggestions should be visible")
	}

	cmd := m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))

	if got := bot.submittedInputs(); len(got) != 0 {
		t.Fatalf("submitted inputs = %#v, want no submission while accepting a suggestion", got)
	}
	if cmd != nil {
		t.Fatal("accepting a suggestion should not start the processing tick")
	}
	if got := m.Input.Value(); got != "/help" {
		t.Fatalf("input value = %q, want selected /help command", got)
	}
	if m.Suggestions.Visible() {
		t.Fatal("suggestions should close after accepting a command")
	}

	m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: 'v', Text: "verbose"}))
	if got := m.Input.Value(); got != "/help verbose" {
		t.Fatalf("input value after typing an argument = %q, want %q", got, "/help verbose")
	}

	cmd = m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if got := bot.submittedInputs(); len(got) != 1 || got[0] != "/help verbose" {
		t.Fatalf("submitted inputs = %#v, want completed command with arguments", got)
	}
	if cmd == nil {
		t.Fatal("second enter should start the processing tick")
	}
}

func TestEnterOpensCommandMenuAndSubmitsLeafChoice(t *testing.T) {
	bot := &commandFakeBot{
		commands: []string{"/model"},
		menus: map[string]controlruntime.CommandMenu{
			"/model": {
				Title: "Choose model",
				Items: []controlruntime.CommandMenuItem{
					{Value: "/model fast", Label: "fast", Description: "openai / gpt-fast", Submit: true},
				},
			},
		},
	}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.Input.SetValue("/model")

	if cmd := m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})); cmd != nil {
		t.Fatal("opening a command menu started a run")
	}
	if !m.Suggestions.IsMenu() || !strings.Contains(m.Suggestions.View(80), "Choose model") {
		t.Fatalf("model menu did not open:\n%s", m.Suggestions.View(80))
	}

	cmd := m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if cmd == nil {
		t.Fatal("selecting a leaf choice did not start a run")
	}
	if got := bot.submittedInputs(); len(got) != 1 || got[0] != "/model fast" {
		t.Fatalf("submitted inputs = %#v", got)
	}
}

func TestSubmitSuggestionPreservesImageDraftWithoutSendingAttachment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bot := &imageCommandFakeBot{commandFakeBot: commandFakeBot{menus: map[string]controlruntime.CommandMenu{
		"/model": {Items: []controlruntime.CommandMenuItem{{Value: "/model fast", Label: "fast", Submit: true}}},
	}}}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatal(err)
	}
	m.Input.SetValue("/model")
	m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	path, err := attachment.SaveImage("session_1", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Input.AddImage(path) {
		t.Fatal("AddImage failed")
	}
	draft := m.Input.Value()

	m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("suggestion submit deleted draft image: %v", err)
	}
	if m.Input.Value() != draft || len(m.Input.ImageAttachments()) != 1 {
		t.Fatalf("suggestion submit discarded image draft: value=%q images=%#v", m.Input.Value(), m.Input.ImageAttachments())
	}
	if len(bot.inputs) != 1 || bot.inputs[0].Text != "/model fast" || len(bot.inputs[0].Images) != 0 {
		t.Fatalf("submitted inputs = %#v", bot.inputs)
	}
}

func TestSubmitLocalSuggestionPreservesImageDraft(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, err := NewModel(&localMenuFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.Input.SetValue("/local")
	m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	path, err := attachment.SaveImage("session_1", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Input.AddImage(path) {
		t.Fatal("AddImage failed")
	}
	draft := m.Input.Value()

	m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("local suggestion deleted draft image: %v", err)
	}
	if m.Input.Value() != draft || len(m.Input.ImageAttachments()) != 1 {
		t.Fatalf("local suggestion discarded image draft: value=%q images=%#v", m.Input.Value(), m.Input.ImageAttachments())
	}
}

func TestProcessingSubmitSuggestionPreservesImageDraftWithoutSendingAttachment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bot := &imageCommandFakeBot{commandFakeBot: commandFakeBot{menus: map[string]controlruntime.CommandMenu{
		"/model": {Items: []controlruntime.CommandMenuItem{{Value: "/model fast", Label: "fast", Submit: true}}},
	}}}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatal(err)
	}
	m.transitionTo(stateProcessing)
	m.Input.SetValue("/model")
	m.handleProcessingKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	path, err := attachment.SaveImage("session_1", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Input.AddImage(path) {
		t.Fatal("AddImage failed")
	}
	draft := m.Input.Value()

	m.handleProcessingKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("processing suggestion deleted draft image: %v", err)
	}
	if m.Input.Value() != draft || len(m.Input.ImageAttachments()) != 1 {
		t.Fatalf("processing suggestion discarded image draft: value=%q images=%#v", m.Input.Value(), m.Input.ImageAttachments())
	}
	if len(bot.steered) != 1 || bot.steered[0].Text != "/model fast" || len(bot.steered[0].Images) != 0 {
		t.Fatalf("steered inputs = %#v", bot.steered)
	}
}

func TestEnterOnCurrentMenuItemHighlightsWithoutSubmitting(t *testing.T) {
	bot := &commandFakeBot{
		commands: []string{"/model"},
		menus: map[string]controlruntime.CommandMenu{
			"/model": {
				Title: "Choose model",
				Items: []controlruntime.CommandMenuItem{
					{Value: "/model flash", Label: "flash", Description: "zai / glm-flash", Submit: true, Current: true},
					{Value: "/model pro", Label: "pro", Description: "zai / glm-pro", Submit: true},
				},
			},
		},
	}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.Input.SetValue("/model")
	m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))

	if view := m.Suggestions.View(80); !strings.Contains(view, "✓") {
		t.Fatalf("current model row missing check mark:\n%s", view)
	}

	cmd := m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if cmd != nil {
		t.Fatal("entering on the current model started a run")
	}
	if got := bot.submittedInputs(); len(got) != 0 {
		t.Fatalf("submitted inputs = %#v, want no submission for the current model", got)
	}
	if !m.Suggestions.IsMenu() {
		t.Fatal("menu should stay open after refusing the current model")
	}
	if view := m.Suggestions.View(80); !strings.Contains(view, "already current") {
		t.Fatalf("missing 'already current' hint:\n%s", view)
	}

	m.handleKeyPress(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if view := m.Suggestions.View(80); strings.Contains(view, "already current") {
		t.Fatalf("hint should clear after moving:\n%s", view)
	} else if !strings.Contains(view, "✓") {
		t.Fatalf("current model row should keep its check mark after moving:\n%s", view)
	}
	m.handleKeyPress(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if got := bot.submittedInputs(); len(got) != 1 || got[0] != "/model pro" {
		t.Fatalf("submitted inputs = %#v, want /model pro", got)
	}
}

func TestNestedCommandMenuEscReturnsToParent(t *testing.T) {
	bot := &commandFakeBot{menus: map[string]controlruntime.CommandMenu{
		"/plugin": {
			Title: "Plugin action",
			Items: []controlruntime.CommandMenuItem{{Value: "/plugin enable", Label: "Enable"}},
		},
		"/plugin enable": {
			Title: "Choose plugin",
			Items: []controlruntime.CommandMenuItem{{Value: "/plugin enable demo", Label: "demo", Submit: true}},
		},
	}}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.Input.SetValue("/plugin")
	m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if !strings.Contains(m.Suggestions.View(80), "Choose plugin") {
		t.Fatalf("nested menu did not open:\n%s", m.Suggestions.View(80))
	}
	m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.Input.Value() != "/plugin" || !strings.Contains(m.Suggestions.View(80), "Plugin action") {
		t.Fatalf("escape did not restore parent: input=%q\n%s", m.Input.Value(), m.Suggestions.View(80))
	}
}

func TestSessionMenuDeletesHighlightedSessionAfterConfirmation(t *testing.T) {
	bot := &sessionDeleteFakeBot{commandFakeBot: commandFakeBot{menus: map[string]controlruntime.CommandMenu{
		"/sessions": {
			Title: "Resume session",
			Items: []controlruntime.CommandMenuItem{
				{Key: "session_1", Value: "/sessions session_1", Label: "session_1", Submit: true},
				{Key: "session_2", Value: "/sessions session_2", Label: "session_2", Submit: true},
			},
		},
	}}}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.Input.SetValue("/sessions")
	m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if !strings.Contains(m.Suggestions.View(80), "d delete") {
		t.Fatalf("session menu does not advertise delete action:\n%s", m.Suggestions.View(80))
	}
	m.handleKeyPress(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))

	m.handleKeyPress(tea.KeyPressMsg(tea.Key{Code: 'd', Text: "d"}))
	if m.state != stateConfirming || !strings.Contains(m.ConfirmBar.View(80, 24), "session_2") {
		t.Fatalf("delete confirmation not opened for highlighted session:\n%s", m.ConfirmBar.View(80, 24))
	}
	if len(bot.deleted) != 0 {
		t.Fatalf("deleted before confirmation: %#v", bot.deleted)
	}

	m.handleConfirmKey(tea.KeyPressMsg(tea.Key{Code: 'y', Text: "y"}))
	if len(bot.deleted) != 1 || bot.deleted[0] != "session_2" {
		t.Fatalf("deleted = %#v, want session_2", bot.deleted)
	}
	if m.state != stateReady || !m.Suggestions.IsMenu() {
		t.Fatal("session menu should reopen after deletion")
	}
}

func TestSessionDeleteConfirmationCanBeCancelled(t *testing.T) {
	bot := &sessionDeleteFakeBot{commandFakeBot: commandFakeBot{menus: map[string]controlruntime.CommandMenu{
		"/sessions": {
			Items: []controlruntime.CommandMenuItem{{Key: "session_1", Value: "/sessions session_1", Label: "session_1", Submit: true}},
		},
	}}}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.Input.SetValue("/sessions")
	m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m.handleKeyPress(tea.KeyPressMsg(tea.Key{Code: 'd', Text: "d"}))
	m.handleConfirmKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))

	if len(bot.deleted) != 0 {
		t.Fatalf("cancelled deletion called runtime: %#v", bot.deleted)
	}
	if m.state != stateReady || !m.Suggestions.IsMenu() {
		t.Fatal("session menu should reopen after cancellation")
	}
}

func TestSessionDeleteFailureIsShownAndMenuReopens(t *testing.T) {
	bot := &sessionDeleteFakeBot{
		commandFakeBot: commandFakeBot{menus: map[string]controlruntime.CommandMenu{
			"/sessions": {
				Items: []controlruntime.CommandMenuItem{{Key: "session_1", Value: "/sessions session_1", Label: "session_1", Submit: true}},
			},
		}},
		deleteErr: errors.New("session is locked"),
	}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.Input.SetValue("/sessions")
	m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m.handleKeyPress(tea.KeyPressMsg(tea.Key{Code: 'd', Text: "d"}))
	m.handleConfirmKey(tea.KeyPressMsg(tea.Key{Code: 'y', Text: "y"}))

	items := m.Messages.Items()
	if len(items) != 1 {
		t.Fatalf("message count = %d, want deletion error", len(items))
	}
	errItem, ok := items[0].(*message.ErrorMessageItem)
	if !ok || !strings.Contains(ansi.Strip(errItem.Render(80)), "session is locked") {
		t.Fatalf("deletion error not rendered: %#v", items[0])
	}
	if !m.Suggestions.IsMenu() {
		t.Fatal("session menu should reopen after deletion failure")
	}
}

func TestCommittedSessionDeleteCleanupFailureIsShownAsWarning(t *testing.T) {
	bot := &sessionDeleteFakeBot{
		commandFakeBot: commandFakeBot{menus: map[string]controlruntime.CommandMenu{
			"/sessions": {
				Items: []controlruntime.CommandMenuItem{{Key: "session_1", Value: "/sessions session_1", Label: "session_1", Submit: true}},
			},
		}},
		deleteErr: committedSessionDeleteError{errors.New("attachment cleanup failed")},
	}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.Input.SetValue("/sessions")
	m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m.handleKeyPress(tea.KeyPressMsg(tea.Key{Code: 'd', Text: "d"}))
	m.handleConfirmKey(tea.KeyPressMsg(tea.Key{Code: 'y', Text: "y"}))

	items := m.Messages.Items()
	if len(items) != 1 {
		t.Fatalf("message count = %d, want cleanup warning", len(items))
	}
	rendered := ansi.Strip(items[0].Render(100))
	if !strings.Contains(rendered, "已删除") || !strings.Contains(rendered, "清理") || strings.Contains(rendered, "删除会话 session_1 失败") {
		t.Fatalf("committed deletion rendered as failure: %q", rendered)
	}
	if !m.Suggestions.IsMenu() {
		t.Fatal("session menu should reopen after committed deletion warning")
	}
}

func TestDStillTypesOutsideSessionMenu(t *testing.T) {
	m, err := NewModel(&tickFakeBot{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.handleIdleKey(tea.KeyPressMsg(tea.Key{Code: 'd', Text: "d"}))
	if got := m.Input.Value(); got != "d" {
		t.Fatalf("input = %q, want d", got)
	}
}

func TestCtrlCClearsPopulatedInputBeforeQuitting(t *testing.T) {
	bot := &commandFakeBot{commands: []string{"/help", "/status"}}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.Input.SetValue("/h")
	m.refreshSuggestions()
	if !m.Suggestions.Visible() {
		t.Fatal("command suggestions should be visible before clear")
	}

	cmd := m.handleKeyPress(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if cmd != nil {
		t.Fatal("ctrl+c with input should clear instead of quitting")
	}
	if m.Input.HasContent() || m.Input.Value() != "" {
		t.Fatalf("input after ctrl+c = %q, want empty", m.Input.Value())
	}
	if m.Suggestions.Visible() {
		t.Fatal("ctrl+c should close command suggestions")
	}
}

func TestCtrlCWithEmptyInputQuits(t *testing.T) {
	m, err := NewModel(&tickFakeBot{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}

	cmd := m.handleKeyPress(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if cmd == nil {
		t.Fatal("ctrl+c with empty input should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c command returned %T, want tea.QuitMsg", cmd())
	}
}

func TestCtrlCClearsSteeringInputWithoutLeavingProcessingState(t *testing.T) {
	m, err := NewModel(&tickFakeBot{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.transitionTo(stateProcessing)
	m.Input.SetValue("additional direction")

	if cmd := m.handleKeyPress(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl})); cmd != nil {
		t.Fatal("ctrl+c with steering input should not quit")
	}
	if m.Input.HasContent() || m.state != stateProcessing {
		t.Fatalf("after clear: input=%q state=%v, want empty processing state", m.Input.Value(), m.state)
	}
}

func TestEnterSubmitsExpandedLargePaste(t *testing.T) {
	bot := &tickFakeBot{}
	m, err := NewModel(bot)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	content := strings.Repeat("pasted diagnostic line\n", 12) + "last line"
	model, _ := m.Update(tea.PasteMsg{Content: content})
	m = model.(*Model)

	if cmd := m.handleKeyPress(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})); cmd == nil {
		t.Fatal("enter after a large paste should start a run")
	}
	if got := bot.submittedInputs(); len(got) != 1 || got[0] != content {
		t.Fatalf("submitted input did not preserve pasted content: got %d entries", len(got))
	}
}

func TestJevPreviewRoutesByCallIDAndSurvivesCompletion(t *testing.T) {
	m, err := NewModel(&statusFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	send := func(kind controlruntime.EventType, id, sub, preview, output string, decision protocol.ToolDecision) {
		m.handleRuntimeEvent(controlruntime.Event{Type: kind, Payload: controlruntime.ToolPayload{CallID: id, ToolName: "shell", SubAgentID: sub, Preview: preview, Output: output, Decision: decision}})
	}
	send(controlruntime.EventToolStarted, "first", "", "", "", "")
	send(controlruntime.EventToolStarted, "first", "child", "", "", "")
	send(controlruntime.EventToolPreview, "first", "child", "make all", "", protocol.ToolDecisionJevSafe)
	items := m.Messages.Items()
	p := items[0].(*processing.ProcessingItem)
	blocks := p.Blocks()
	if blocks[0].JevNote != "" || !strings.Contains(blocks[1].JevNote, "Jev") {
		t.Fatalf("preview routed to wrong call: %+v", blocks)
	}
	if view := ansi.Strip(p.Render(100)); !strings.Contains(view, "Jev 判定安全") {
		t.Fatalf("live verdict missing: %s", view)
	}
	send(controlruntime.EventToolCompleted, "first", "child", "", "second result", "")
	send(controlruntime.EventToolCompleted, "first", "", "", "first result", "")
	blocks = p.Blocks()
	if blocks[0].Content != "first result" || blocks[1].Content != "second result" || blocks[1].JevNote == "" {
		t.Fatalf("completion lost identity/verdict: %+v", blocks)
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunDone})
	var view strings.Builder
	for _, item := range m.Messages.Items() {
		view.WriteString(ansi.Strip(item.Render(100)))
	}
	if !strings.Contains(view.String(), "Jev 判定安全") || !strings.Contains(view.String(), "second result") {
		t.Fatalf("settled verdict missing: %s", view.String())
	}
}
