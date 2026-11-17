package runtime

import (
	"context"
	"strings"
	"testing"

	"nekocode/mode"
)

func TestSessionRestorePreservesCommittedSelection(t *testing.T) {
	current := "main"
	newCalls := 0
	services := ServiceGroups{Sessions: SessionsServices{
		CurrentSessionID: func() string { return current }, ListSessions: func() []SessionMeta { return nil }, SessionMessages: func() []DisplayMessage { return nil },
		ResumeSession: func(id string) error {
			current = id
			if id == "main" {
				return committedTestError{}
			}
			return nil
		},
		NewSession: func() (SessionMeta, error) {
			newCalls++
			current = "unexpected_fallback"
			return SessionMeta{ID: current}, nil
		},
	}}
	r := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "done", nil }), services)
	defer r.Close()
	id, _, err := r.StartRunInSession(t.Context(), "a2a", Input{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.WaitRun(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if current != "main" || newCalls != 0 {
		t.Fatalf("committed restore was replaced: current=%q newSessionCalls=%d", current, newCalls)
	}
	if r.Status().State != RuntimeReady {
		t.Fatal("committed housekeeping error blocked the restored runtime")
	}
	id, err = r.StartRun(t.Context(), Input{Text: "normal run after committed restore"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.WaitRun(t.Context(), id); err != nil {
		t.Fatal(err)
	}
}

func TestScopedDraftLifetimeFollowsSessionDeletion(t *testing.T) {
	current := "main"
	r := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "done", nil }), ServiceGroups{Sessions: SessionsServices{
		CurrentSessionID: func() string { return current },
		ListSessions:     func() []SessionMeta { return nil }, SessionMessages: func() []DisplayMessage { return nil },
		ResumeSession: func(id string) error { current = id; return nil },
		NewSession:    func() (SessionMeta, error) { current = "scoped"; return SessionMeta{ID: current}, nil },
		DeleteSession: func(string) error { return nil },
		CaptureDraft:  func() (func() error, error) { id := current; return func() error { current = id; return nil }, nil },
	}})
	defer r.Close()
	id, selected, err := r.StartRunInSession(t.Context(), "", Input{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.WaitRun(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if current != "main" || len(r.sessionDrafts) != 1 || r.sessionDrafts[selected] == nil {
		t.Fatal("draft capture or foreground restoration failed")
	}
	if err := r.DeleteInactiveSession(selected); err != nil {
		t.Fatal(err)
	}
	if len(r.sessionDrafts) != 0 {
		t.Fatal("deleted session retained its draft")
	}
}

func TestSessionModeCommandDoesNotLockContext(t *testing.T) {
	runner := &sessionCommandRunner{current: "main"}
	selected := map[string]mode.ID{}
	currentMode := func() mode.ID {
		if selected[runner.current] == "" {
			return mode.Standard
		}
		return selected[runner.current]
	}
	services := sessionRunnerServices(runner)
	services.Modes = ModesServices{Current: currentMode, Options: mode.Definitions, Set: func(id mode.ID) error { selected[runner.current] = id; return nil }, Resolve: func(string) (mode.Snapshot, error) { d, err := mode.Resolve(currentMode()); return d.Snapshot, err }}
	runner.command = func(input string, _ RunHost) CommandResult {
		switch input {
		case "/mode super-agent":
			selected[runner.current] = mode.SuperAgent
		case "/mode standard":
			selected[runner.current] = mode.Standard
		default:
			return CommandResult{Action: CommandIgnored}
		}
		return CommandResult{Action: CommandHandled}
	}
	r := New(runner, services)
	defer r.Close()
	if _, _, err := r.StartRunInSession(t.Context(), "a2a", Input{Text: "/mode super-agent"}); err == nil {
		t.Fatal("unsupported mode command was allowed to change the session")
	}
	if selected["a2a"] == mode.SuperAgent {
		t.Fatal("rejected command changed the mode")
	}
	// Recover a Context persisted by an earlier version or another transport.
	selected["a2a"] = mode.SuperAgent
	id, _, err := r.StartRunInSession(t.Context(), "a2a", Input{Text: "/mode standard"})
	if err != nil {
		t.Fatalf("context cannot recover after mode command: %v", err)
	}
	if err := r.WaitRun(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if selected["a2a"] != mode.Standard || runner.current != "main" {
		t.Fatal("mode recovery failed or changed the main session")
	}
}

func TestSessionRunRejectsCommandsThatChangeContextOwnership(t *testing.T) {
	for _, input := range []string{"/new", "/fork", "/fork branch", "/sessions main", "/sessions delete main"} {
		t.Run(input, func(t *testing.T) {
			runner := &sessionCommandRunner{current: "main"}
			calls := 0
			runner.command = func(string, RunHost) CommandResult { calls++; return CommandResult{Action: CommandHandled} }
			r := New(runner, sessionRunnerServices(runner))
			defer r.Close()
			if _, _, err := r.StartRunInSession(t.Context(), "context_session", Input{Text: input}); err == nil || !strings.Contains(err.Error(), "contextId") {
				t.Fatalf("unsupported session command: %v", err)
			}
			if calls != 0 || runner.current != "main" {
				t.Fatal("rejected command changed session state")
			}
			id, err := r.StartRun(t.Context(), Input{Text: input})
			if err != nil {
				t.Fatalf("normal command behavior changed: %v", err)
			}
			if err := r.WaitRun(t.Context(), id); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("normal command calls=%d", calls)
			}
		})
	}
}
