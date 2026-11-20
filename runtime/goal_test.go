package runtime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"nekocode/protocol"
)

type interactiveGoalRunner struct {
	calls     atomic.Int32
	previous  atomic.Value
	continued chan RunID
}

func (g *interactiveGoalRunner) Run(ctx context.Context, _ string, _ RunHost) (string, error) {
	id := protocol.ExecutionFromContext(ctx).RunID
	if g.calls.Add(1) == 1 {
		g.previous.Store(id)
	} else if protocol.IsGoalContinuation(ctx) {
		g.continued <- RunID(id)
	}
	return "progress", nil
}
func (g *interactiveGoalRunner) PendingGoalRun(id string) bool { return id == g.previous.Load() }

func TestGoalIdleAdmissionStartsOneContinuation(t *testing.T) {
	runner := &interactiveGoalRunner{continued: make(chan RunID, 2)}
	rt := New(runner, ServiceGroups{})
	defer rt.Close()
	first, err := rt.StartRun(t.Context(), Input{Text: "work", Source: SourceRef{Kind: "tui"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case next := <-runner.continued:
		if next == first {
			t.Fatal("reused run identity")
		}
		waitForRun(t, rt, next)
		started := rt.events.History(EventFilter{RunID: next, Types: []EventType{EventRunStarted}})
		if len(started) != 1 || started[0].Source.Kind != "goal_continuation" {
			t.Fatalf("missing goal continuation lifecycle: %+v", started)
		}
		if len(rt.events.History(EventFilter{RunID: next, Types: []EventType{EventInputAccepted}})) != 0 {
			t.Fatal("automatic turn presented as user input")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("continuation missing")
	}
}

type pausableGoalRunner struct {
	started chan struct{}
}

func (p *pausableGoalRunner) ActiveSessionGoal() bool { return true }
func (p *pausableGoalRunner) Run(ctx context.Context, _ string, _ RunHost) (string, error) {
	close(p.started)
	<-ctx.Done()
	return "", ctx.Err()
}

func TestGoalPauseCancelsRunningGoal(t *testing.T) {
	runner := &pausableGoalRunner{started: make(chan struct{})}
	rt := New(runner, ServiceGroups{Commands: CommandsServices{ExecuteLocalCommand: func(context.Context, string) (string, LocalCommandResult) { return "", LocalCommandRequiresIdle }}})
	defer rt.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	id, err := rt.StartRun(ctx, Input{Text: "work", Source: SourceRef{Kind: "tui"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, result := rt.ExecuteLocalCommand(ctx, "/GOAL  PAUSE")
	if result != LocalCommandExecuted {
		t.Fatal(result)
	}
	if err := rt.WaitRun(ctx, id); err != nil {
		t.Fatal(err)
	}
	if events := rt.events.History(EventFilter{RunID: id, Types: []EventType{EventRunCancelled}}); len(events) != 1 {
		t.Fatal(events)
	}
}

func TestGoalAdmissionUsesSessionCreatedByFirstRun(t *testing.T) {
	runner := &interactiveGoalRunner{continued: make(chan RunID, 1)}
	// Deleting the current session leaves no identity until the bot handles the
	// first command. Its runtime admission precedes creating the new session.
	rt := New(runner, ServiceGroups{Sessions: SessionsServices{ListSessions: func() []SessionMeta { return nil }, SessionMessages: func() []DisplayMessage { return nil }, CurrentSessionID: func() string {
		if runner.calls.Load() == 0 {
			return ""
		}
		return "new-session"
	}}})
	defer rt.Close()
	if _, err := rt.StartRun(t.Context(), Input{Text: "/goal work", Source: SourceRef{Kind: "gui"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-runner.continued:
		waitForRun(t, rt, id)
	case <-time.After(time.Second):
		t.Fatal("goal never continued in its newly created session")
	}
}
