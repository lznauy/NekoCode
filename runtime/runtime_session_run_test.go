package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"nekocode/mode"
)

func TestStartRunInSessionRestoresPreviousSession(t *testing.T) {
	runner := &sessionCommandRunner{current: "main_session"}
	runner.run = func(_ string, _ RunHost) (string, error) {
		if runner.current != "a2a_session" {
			t.Errorf("run session = %q, want a2a_session", runner.current)
		}
		return "done", nil
	}
	runtime := New(runner, sessionRunnerServices(runner))
	defer runtime.Close()
	runID, sessionID, err := runtime.StartRunInSession(context.Background(), "a2a_session", Input{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if sessionID != "a2a_session" {
		t.Fatalf("selected session = %q", sessionID)
	}
	if err := runtime.WaitRun(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
	if runner.current != "main_session" {
		t.Fatalf("restored session = %q, want main_session", runner.current)
	}
}

func TestSessionRunDoesNotSwitchSessionDuringNormalRun(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	runner := &sessionCommandRunner{current: "main_session"}
	runner.run = func(string, RunHost) (string, error) { close(started); <-release; return "done", nil }
	rt := New(runner, sessionRunnerServices(runner))
	defer rt.Close()
	runID, err := rt.StartRun(t.Context(), Input{Text: "normal run"})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	_, _, a2aErr := rt.StartRunInSession(t.Context(), "a2a_session", Input{Text: "must not switch"})
	current := runner.current
	close(release)
	if err := rt.WaitRun(t.Context(), runID); err != nil {
		t.Fatal(err)
	}
	if a2aErr == nil || current != "main_session" {
		t.Fatalf("session changed during existing run: current=%q err=%v", current, a2aErr)
	}
}

func TestSessionRunDoesNotSwitchSessionDuringBackgroundWork(t *testing.T) {
	runner := &sessionCommandRunner{current: "main_session"}
	rt := New(runner, sessionRunnerServices(runner))
	rt.background.mu.Lock()
	rt.background.active = 1
	rt.background.mu.Unlock()
	defer func() { rt.background.mu.Lock(); rt.background.active = 0; rt.background.mu.Unlock(); _ = rt.Close() }()
	if _, _, err := rt.StartRunInSession(t.Context(), "a2a_session", Input{Text: "must not switch"}); err == nil {
		t.Fatal("session switched during background work")
	}
	if runner.current != "main_session" {
		t.Fatalf("current=%q", runner.current)
	}
}

func TestSessionRunRejectsSuperAgentWithoutChangingNormalAdmission(t *testing.T) {
	runner := &sessionCommandRunner{current: "main_session"}
	services := sessionRunnerServices(runner)
	services.Modes = ModesServices{
		Current: func() mode.ID { return mode.SuperAgent }, Options: mode.Definitions, Set: func(mode.ID) error { return nil },
		Resolve: func(string) (mode.Snapshot, error) {
			definition, err := mode.Resolve(mode.SuperAgent)
			return definition.Snapshot, err
		},
	}
	rt := New(runner, services)
	defer rt.Close()
	if _, _, err := rt.StartRunInSession(t.Context(), "a2a_session", Input{Text: "scoped run"}); err == nil {
		t.Fatal("Super Agent admitted as session-scoped run")
	}
	if runner.current != "main_session" {
		t.Fatalf("main session not restored: %q", runner.current)
	}
	id, err := rt.StartRun(t.Context(), Input{Text: "normal Super Agent run"})
	if err != nil {
		t.Fatalf("normal admission changed: %v", err)
	}
	if err := rt.WaitRun(t.Context(), id); err != nil {
		t.Fatal(err)
	}
}

func TestStartRunInSessionRestoresEmptySessionWithFreshIdentity(t *testing.T) {
	current := ""
	next := 0
	services := SessionsServices{
		CurrentSessionID: func() string { return current },
		ResumeSession:    func(id string) error { current = id; return nil },
		NewSession: func() (SessionMeta, error) {
			next++
			current = fmt.Sprintf("session_%d", next)
			return SessionMeta{ID: current}, nil
		},
		ListSessions:    func() []SessionMeta { return nil },
		SessionMessages: func() []DisplayMessage { return nil },
	}
	runtime := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "done", nil }), ServiceGroups{Sessions: services})
	defer runtime.Close()
	runID, selected, err := runtime.StartRunInSession(context.Background(), "", Input{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.WaitRun(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
	if selected != "session_1" || current != "session_2" {
		t.Fatalf("selected=%q restored=%q", selected, current)
	}
}

func TestStartRunInSessionRestoresSessionAfterCancellation(t *testing.T) {
	current := "main_session"
	started := make(chan struct{})
	services := SessionsServices{
		CurrentSessionID: func() string { return current },
		ResumeSession:    func(id string) error { current = id; return nil },
		NewSession:       func() (SessionMeta, error) { return SessionMeta{ID: "new_session"}, nil },
		ListSessions:     func() []SessionMeta { return nil },
		SessionMessages:  func() []DisplayMessage { return nil },
	}
	runtime := New(RunnerFunc(func(ctx context.Context, _ string, _ RunHost) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}), ServiceGroups{Sessions: services})
	defer runtime.Close()
	runID, _, err := runtime.StartRunInSession(context.Background(), "a2a_session", Input{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if err := runtime.CancelRun(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
	if err := runtime.WaitRun(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
	if current != "main_session" {
		t.Fatalf("restored session = %q, want main_session", current)
	}
}

func TestStartRunInSessionFallsBackWhenRestoreFails(t *testing.T) {
	current := "main_session"
	services := SessionsServices{
		CurrentSessionID: func() string { return current },
		ResumeSession: func(id string) error {
			if id == "main_session" {
				return errors.New("session disappeared")
			}
			current = id
			return nil
		},
		NewSession: func() (SessionMeta, error) {
			current = "fallback_session"
			return SessionMeta{ID: current}, nil
		},
		ListSessions:    func() []SessionMeta { return nil },
		SessionMessages: func() []DisplayMessage { return nil },
	}
	runtime := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "done", nil }), ServiceGroups{Sessions: services})
	defer runtime.Close()
	runID, _, err := runtime.StartRunInSession(context.Background(), "a2a_session", Input{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.WaitRun(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
	if current != "fallback_session" {
		t.Fatalf("current session = %q, want fallback_session", current)
	}
}

func TestStartRunInSessionRestoresPreviousSessionAfterPartialSelectionFailure(t *testing.T) {
	current := "main_session"
	services := SessionsServices{
		CurrentSessionID: func() string { return current },
		ResumeSession: func(id string) error {
			current = id
			if id == "a2a_session" {
				return errors.New("failed after activating session")
			}
			return nil
		},
		NewSession: func() (SessionMeta, error) {
			current = "fallback_session"
			return SessionMeta{ID: current}, nil
		},
		ListSessions:    func() []SessionMeta { return nil },
		SessionMessages: func() []DisplayMessage { return nil },
	}
	runtime := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "done", nil }), ServiceGroups{Sessions: services})
	defer runtime.Close()
	if _, _, err := runtime.StartRunInSession(context.Background(), "a2a_session", Input{Text: "hello"}); err == nil {
		t.Fatal("partially failed session selection succeeded")
	}
	if current != "main_session" {
		t.Fatalf("current session = %q, want main_session", current)
	}
	runID, err := runtime.StartRun(context.Background(), Input{Text: "run after rollback"})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.WaitRun(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
}

func TestStartRunInSessionBlocksRunsUntilFailedRestoreIsRecovered(t *testing.T) {
	current := "main_session"
	newSessionFails := true
	services := SessionsServices{
		CurrentSessionID: func() string { return current },
		ResumeSession: func(id string) error {
			if id == "main_session" {
				return errors.New("session disappeared")
			}
			current = id
			return nil
		},
		NewSession: func() (SessionMeta, error) {
			if newSessionFails {
				return SessionMeta{}, errors.New("cannot create fallback")
			}
			current = "recovered_session"
			return SessionMeta{ID: current}, nil
		},
		ListSessions:    func() []SessionMeta { return nil },
		SessionMessages: func() []DisplayMessage { return nil },
	}
	runtime := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "done", nil }), ServiceGroups{Sessions: services})
	defer runtime.Close()
	runID, _, err := runtime.StartRunInSession(context.Background(), "a2a_session", Input{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.WaitRun(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.StartRun(context.Background(), Input{Text: "must not run"}); err == nil {
		t.Fatal("run started while the current session was left in an invalid state")
	}
	if status := runtime.Status(); status.State != RuntimeBusy {
		t.Fatalf("state = %s, want %s until session recovery", status.State, RuntimeBusy)
	}

	newSessionFails = false
	if _, err := runtime.NewSession(); err != nil {
		t.Fatal(err)
	}
	recoveredRunID, err := runtime.StartRun(context.Background(), Input{Text: "run after recovery"})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.WaitRun(context.Background(), recoveredRunID); err != nil {
		t.Fatal(err)
	}
}
