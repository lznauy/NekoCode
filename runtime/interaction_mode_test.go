package runtime

import (
	"context"
	"nekocode/mode"
	"testing"
)

func TestRunCapturesModeAndRejectsMidRunSwitch(t *testing.T) {
	selected := mode.Plan
	started := make(chan mode.Snapshot, 1)
	release := make(chan struct{})
	rt := New(RunnerFunc(func(ctx context.Context, _ string, _ RunHost) (string, error) {
		snapshot, _ := mode.FromContext(ctx)
		started <- snapshot
		<-release
		return "done", nil
	}), ServiceGroups{Modes: ModesServices{
		Current: func() mode.ID { return selected }, Options: mode.Definitions,
		Set:     func(id mode.ID) error { selected = id; return nil },
		Resolve: func(string) (mode.Snapshot, error) { d, err := mode.Resolve(selected); return d.Snapshot, err },
	}})
	t.Cleanup(func() { _ = rt.Close() })
	id, err := rt.StartRun(context.Background(), Input{Text: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	captured := <-started
	if captured.ID != mode.Plan {
		t.Fatalf("missing captured mode: %+v", captured)
	}
	if err := rt.SetInteractionMode(mode.Standard); err == nil {
		t.Fatal("mid-run switch accepted")
	}
	close(release)
	if err := rt.WaitRun(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if err := rt.SetInteractionMode(mode.Standard); err != nil {
		t.Fatal(err)
	}
	snapshot, ok := rt.LookupRun(id)
	if !ok || snapshot.InteractionMode != captured {
		t.Fatalf("historical run followed selection: %+v", snapshot)
	}
}

func TestUnknownModeCannotReachRunnerButControlCommandsRemainUsable(t *testing.T) {
	selected := mode.ID("unknown")
	called := false
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { called = true; return "", nil }), ServiceGroups{
		Modes: ModesServices{Current: func() mode.ID { return selected }, Options: mode.Definitions, Set: func(id mode.ID) error { selected = id; return nil }, Resolve: func(string) (mode.Snapshot, error) { d, err := mode.Resolve(selected); return d.Snapshot, err }},
		Commands: CommandsServices{ExecuteCommand: func(_ context.Context, input string, _ RunHost) (CommandResult, error) {
			if input == "/mode standard" {
				selected = mode.Standard
				return CommandResult{Action: CommandHandled}, nil
			}
			return CommandResult{Action: CommandIgnored}, nil
		}, CommandMenu: func(context.Context, string) (CommandMenu, bool) { return CommandMenu{}, false }},
	})
	defer rt.Close()
	id, err := rt.StartRun(context.Background(), Input{Text: "execute"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := rt.LookupRun(id)
	if called || snapshot.Status != RunFailed {
		t.Fatalf("unknown mode ran: %+v", snapshot)
	}
	id, err = rt.StartRun(context.Background(), Input{Text: "/mode standard"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if selected != mode.Standard || called {
		t.Fatal("control recovery was blocked or executed agent")
	}
}
