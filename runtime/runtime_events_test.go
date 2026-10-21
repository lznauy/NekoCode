package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nekocode/runtime/internal/connectors"
	"nekocode/runtime/internal/recording"
)

func TestManagerRedactsSensitiveInputEvents(t *testing.T) {
	rt := newTestRuntime(&testBot{})
	runID, err := rt.StartRun(context.Background(), Input{
		Source: SourceRef{Kind: "test"},
		Text:   "/connect telegram token 123456:super-secret",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitForRun(t, rt, runID)
	run, ok := rt.LookupRun(runID)
	if !ok {
		t.Fatal("run snapshot missing")
	}
	if strings.Contains(run.Input, "super-secret") || !strings.Contains(run.Input, "[redacted]") {
		t.Fatalf("run input was not redacted: %q", run.Input)
	}
}

func TestManagerDisconnectCommandDoesNotDuplicateConnectorStatus(t *testing.T) {
	rt := newTestRuntime(&testBot{})
	rt.RegisterConnector("telegram", func(runtime ConnectorRuntime) connectors.Connector {
		return statusPublishingConnector{rt: runtime}
	})
	if _, err := rt.Connect(context.Background(), "telegram", nil); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "/disconnect telegram"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitForRun(t, rt, runID)

	for _, ev := range rt.events.History(EventFilter{RunID: runID}) {
		if ev.Type == EventSystemMessage {
			t.Fatalf("disconnect command should rely on connector status event, got system message: %#v", ev.Payload)
		}
	}

	var statusMessages int
	for _, ev := range rt.events.History(EventFilter{}) {
		if ev.Type == EventConnectorStatus {
			statusMessages++
		}
	}
	if statusMessages != 1 {
		t.Fatalf("connector status events = %d, want 1", statusMessages)
	}
}

func TestManagerEventRecordingAdvancesRunSequence(t *testing.T) {
	baseDir := t.TempDir()
	recorder, err := recording.NewEventRecorder(baseDir)
	if err != nil {
		t.Fatalf("NewEventRecorder: %v", err)
	}
	now := time.Now()
	recorder.Record(Event{
		ID:     "evt_1",
		RunID:  "run_41",
		Type:   EventInputAccepted,
		Time:   now,
		Source: SourceRef{Kind: "test"},
		Payload: MessagePayload{
			Content: "historical input",
			Source:  SourceRef{Kind: "test"},
		},
	})
	recorder.Record(Event{
		ID:    "evt_2",
		RunID: "run_41",
		Type:  EventRunDone,
		Time:  now.Add(time.Millisecond),
		Payload: RunResult{
			Output: "historical output",
		},
	})

	bot := &testBot{}
	bot.run = func(_ string, _ RunHost) (string, error) {
		return "fresh output", nil
	}
	rt := newTestRuntime(bot)
	if err := rt.EnableEventRecording(baseDir); err != nil {
		t.Fatalf("EnableEventRecording: %v", err)
	}

	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "fresh input"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if !strings.HasPrefix(string(runID), "run_42_") {
		t.Fatalf("run id = %q, want run_42_<unique suffix>", runID)
	}
	waitForRun(t, rt, runID)

	historical, ok := rt.LookupRun("run_41")
	if !ok {
		t.Fatal("historical run missing")
	}
	if historical.Input != "historical input" || historical.Output != "historical output" {
		t.Fatalf("historical run was changed: %#v", historical)
	}
}

func TestManagerEventRecordingRejectsEmptyDirectory(t *testing.T) {
	rt := newTestRuntime(&testBot{})
	if err := rt.EnableEventRecording(""); err == nil {
		t.Fatal("EnableEventRecording accepted an empty directory")
	}
}

func TestManagerEventRecordingIsConfiguredOnceConcurrently(t *testing.T) {
	rt := newTestRuntime(&testBot{})
	baseDir := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- rt.EnableEventRecording(baseDir)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("EnableEventRecording: %v", err)
		}
	}
	if rt.recorder == nil {
		t.Fatal("event recorder was not configured")
	}
}

func TestManagerEventRecordingRejectsClosedRuntime(t *testing.T) {
	rt := newTestRuntime(&testBot{})
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	err := rt.EnableEventRecording(t.TempDir())
	var protocolErr *ProtocolError
	if !errors.As(err, &protocolErr) || protocolErr.Code != ErrorClosed {
		t.Fatalf("EnableEventRecording error = %v, want closed protocol error", err)
	}
}

func TestManagerSteerRedactsSensitiveInputEvents(t *testing.T) {
	bot := &testBot{}
	rt := newTestRuntime(bot)
	started := make(chan struct{})
	release := make(chan struct{})
	bot.run = func(string, RunHost) (string, error) {
		close(started)
		<-release
		return "ok", nil
	}

	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "start"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bot run to start")
	}
	err = rt.SteerRun(context.Background(), runID, Input{
		Source: SourceRef{Kind: "test"},
		Text:   "/connect telegram token 123456:super-secret",
	})
	if err != nil {
		t.Fatalf("Steer: %v", err)
	}

	var found bool
	for _, ev := range rt.events.History(EventFilter{RunID: runID, Types: []EventType{EventInputAccepted}}) {
		p, ok := ev.Payload.(MessagePayload)
		if !ok || !strings.Contains(p.Content, "/connect telegram token") {
			continue
		}
		found = true
		if strings.Contains(p.Content, "super-secret") || !strings.Contains(p.Content, "[redacted]") {
			t.Fatalf("steer input was not redacted: %q", p.Content)
		}
	}
	if !found {
		t.Fatal("redacted steer input event not found")
	}
	close(release)
	waitForRun(t, rt, runID)
}

// Binding the recorder to the session a process serves keeps every turn in one
// directory. Without it each resume — a fresh process per turn for the bot
// front-end — would leave an orphan batch behind.
func TestBindRecordedSessionNamesTheBatch(t *testing.T) {
	baseDir := t.TempDir()
	rt := newTestRuntime(&testBot{})
	if err := rt.EnableEventRecording(baseDir); err != nil {
		t.Fatalf("EnableEventRecording: %v", err)
	}
	const sessionID = "20261018T100000-111111111"
	rt.BindRecordedSession(sessionID)

	runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, rt, runID)

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) != 1 || dirs[0] != sessionID {
		t.Fatalf("batches = %v, want only %s", dirs, sessionID)
	}
	recorded := filepath.Join(baseDir, sessionID, "runs", string(runID), "events.jsonl")
	if _, err := os.Stat(recorded); err != nil {
		t.Fatalf("recorded run not under <session>/runs: %v", err)
	}
}

func TestRunIDsAcrossRuntimesAreUnique(t *testing.T) {
	a, b := newTestRuntime(&testBot{}), newTestRuntime(&testBot{})
	base := t.TempDir()
	// Both processes scan the same empty history before either starts a run.
	for _, rt := range []*Runtime{a, b} {
		if err := rt.EnableEventRecording(base); err != nil {
			t.Fatal(err)
		}
		rt.BindRecordedSession("shared_session")
	}
	first, err := a.StartRun(context.Background(), Input{Text: "one"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.StartRun(context.Background(), Input{Text: "two"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, a, first)
	waitForRun(t, b, second)
	if first == second {
		t.Fatalf("independent runtimes share run ID %s", first)
	}
	events, err := recording.LoadRecordedEvents(base)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[RunID]bool{}
	for _, ev := range events {
		seen[ev.RunID] = true
	}
	if !seen[first] || !seen[second] || len(seen) != 2 {
		t.Fatalf("restored identities = %v", seen)
	}
}

func TestSteeringCannotPrecedeRecordingRegistration(t *testing.T) {
	entered, release, finish := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	bot := &testBot{run: func(string, RunHost) (string, error) { <-finish; return "done", nil }}
	services := testBotServices(bot)
	services.Sessions.ListSessions = func() []SessionMeta { return nil }
	services.Sessions.SessionMessages = func() []DisplayMessage { return nil }
	services.Sessions.CurrentSessionID = func() string { once.Do(func() { close(entered); <-release }); return "session_b" }
	rt := New(bot, services)
	base := t.TempDir()
	if err := rt.EnableEventRecording(base); err != nil {
		t.Fatal(err)
	}
	rt.BindRecordedSession("session_a")
	ids := make(chan RunID, 1)
	go func() {
		id, err := rt.StartRun(context.Background(), Input{Text: "initial"})
		if err != nil {
			t.Error(err)
		}
		ids <- id
	}()
	<-entered
	// It may reject this steer until registration is complete, but must never
	// accept it into the previous session's directory.
	_ = rt.SteerRun(context.Background(), "", Input{Text: "early steer"})
	close(release)
	id := <-ids
	close(finish)
	if err := rt.WaitRun(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "session_a", "runs", string(id), "events.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("run spilled into previous session: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "session_b", "runs", string(id), "events.jsonl")); err != nil {
		t.Fatal(err)
	}
}

func TestRecordingFollowsLazyAndSwitchedSessions(t *testing.T) {
	base := t.TempDir()
	sessionID := ""
	bot := &testBot{run: func(input string, _ RunHost) (string, error) { sessionID = input; return "done", nil }}
	services := testBotServices(bot)
	services.Sessions.CurrentSessionID = func() string { return sessionID }
	services.Sessions.ListSessions = func() []SessionMeta { return nil }
	services.Sessions.SessionMessages = func() []DisplayMessage { return nil }
	rt := New(bot, services)
	if err := rt.EnableEventRecording(base); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"session_a", "session_b"} {
		// Empty identity models a fresh conversation created lazily by Bot.Run.
		sessionID = ""
		runID, err := rt.StartRun(context.Background(), Input{Text: id})
		if err != nil {
			t.Fatal(err)
		}
		if err := rt.WaitRun(context.Background(), runID); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(base, id, "runs", string(runID), "events.jsonl")); err != nil {
			t.Fatal(err)
		}
	}
}

// A second process resuming the same session appends to the same directory.
func TestBindRecordedSessionAppendsToExistingBatch(t *testing.T) {
	baseDir := t.TempDir()
	const sessionID = "20261018T100000-111111111"

	for i := 0; i < 2; i++ {
		rt := newTestRuntime(&testBot{})
		if err := rt.EnableEventRecording(baseDir); err != nil {
			t.Fatalf("EnableEventRecording: %v", err)
		}
		rt.BindRecordedSession(sessionID)
		runID, err := rt.StartRun(context.Background(), Input{Source: SourceRef{Kind: "test"}, Text: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		waitForRun(t, rt, runID)
		if err := rt.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) != 1 || dirs[0] != sessionID {
		t.Fatalf("batches = %v, want a single session dir", dirs)
	}
}
