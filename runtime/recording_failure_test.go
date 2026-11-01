package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"nekocode/protocol"
	"nekocode/runtime/internal/recording"
)

func TestFinalRecordingFailureReachesLiveResultIncludingCancellation(t *testing.T) {
	for _, mode := range []string{"done", "cancel", "close"} {
		t.Run(mode, func(t *testing.T) {
			started := make(chan struct{})
			cause := errors.New("last recording frame failed")
			var executions atomic.Int32
			rt := New(RunnerFunc(func(ctx context.Context, _ string, host RunHost) (string, error) {
				executions.Add(1)
				host.Step(protocol.StepEvent{Action: protocol.StepActionExecuteTool, CallID: "once", ToolName: "write", Output: "executed"})
				close(started)
				if mode != "done" {
					<-ctx.Done()
				}
				return "already produced", nil
			}), ServiceGroups{})
			defer rt.Close()
			if err := rt.EnableEventRecording(t.TempDir()); err != nil {
				t.Fatal(err)
			}
			rec := rt.recorder
			// Fault injection at the single recording boundary: no retry or second
			// terminal event is available to hide failure of the final write itself.
			rt.events.SetRecordingSink(func(event Event) error {
				if event.IsRunTerminal() {
					rec.RecordFailure(event.RunID, cause)
					return cause
				}
				return rec.RecordRunEvent(event)
			})
			id, err := rt.StartRun(t.Context(), Input{Text: "run"})
			if err != nil {
				t.Fatal(err)
			}
			<-started
			switch mode {
			case "cancel":
				if err := rt.CancelRun(t.Context(), id); err != nil {
					t.Fatal(err)
				}
			case "close":
				if err := rt.Close(); !protocol.EvidenceIncomplete(err) || !errors.Is(err, cause) {
					t.Fatalf("Close lost recording cause: %v", err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if err := rt.WaitRun(ctx, id); err != nil {
				t.Fatal(err)
			}
			snapshot, ok := rt.LookupRun(id)
			if !ok || !snapshot.EvidenceIncomplete {
				t.Fatalf("snapshot=%+v", snapshot)
			}
			terminals := 0
			for _, event := range rt.events.History(EventFilter{RunID: id}) {
				if event.IsRunTerminal() {
					terminals++
					result := event.Payload.(RunResult)
					if !result.EvidenceIncomplete {
						t.Fatal("live result omitted recording failure")
					}
					if mode == "done" && (event.Type != EventRunDone || result.Output != "already produced") {
						t.Fatalf("recording changed executed result: %+v", event)
					}
				}
			}
			if executions.Load() != 1 || terminals != 1 {
				t.Fatalf("executions=%d terminals=%d", executions.Load(), terminals)
			}
			if err := rt.Close(); !protocol.EvidenceIncomplete(err) || !errors.Is(err, cause) {
				t.Fatalf("recording failure lost at Close: %v", err)
			}
		})
	}
}

func TestRecordingBindFailureIsPersistedAndDoesNotContaminateNextRun(t *testing.T) {
	root := t.TempDir()
	var session atomic.Value
	session.Store("")
	var executions atomic.Int32
	rt := New(RunnerFunc(func(ctx context.Context, _ string, _ RunHost) (string, error) {
		if executions.Add(1) == 1 {
			id := protocol.ExecutionFromContext(ctx).RunID
			if err := os.MkdirAll(filepath.Join(root, "bound", "runs", id), 0700); err != nil {
				return "", err
			}
			session.Store("bound")
		}
		return "actual result", nil
	}), ServiceGroups{Sessions: SessionsServices{ListSessions: func() []SessionMeta { return nil }, SessionMessages: func() []DisplayMessage { return nil }, CurrentSessionID: func() string { return session.Load().(string) }}})
	defer rt.Close()
	if err := rt.EnableEventRecording(root); err != nil {
		t.Fatal(err)
	}
	for _, broken := range []bool{true, false} {
		id, err := rt.StartRun(t.Context(), Input{Text: "run"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		err = rt.WaitRun(ctx, id)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		snapshot, ok := rt.LookupRun(id)
		if !ok || snapshot.EvidenceIncomplete != broken || snapshot.Status != RunDone || snapshot.Output != "actual result" {
			t.Fatalf("snapshot=%+v", snapshot)
		}
		rt.mu.Lock()
		path := rt.archivedRuns[id].Path
		rt.mu.Unlock()
		var terminal RunResult
		if err := recording.WalkRecordedEventFile(path, func(event Event) {
			if event.IsRunTerminal() {
				terminal = event.Payload.(RunResult)
			}
		}); err != nil {
			t.Fatal(err)
		}
		if terminal.EvidenceIncomplete != broken {
			t.Fatalf("persisted terminal=%+v", terminal)
		}
	}
	if executions.Load() != 2 {
		t.Fatal("binding failure replayed runner")
	}
	if err := rt.Close(); !protocol.EvidenceIncomplete(err) {
		t.Fatalf("binding error missing from Close: %v", err)
	}
}

func TestRecordingRegistrationFailurePreventsExecution(t *testing.T) {
	root := t.TempDir()
	var calls atomic.Int32
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { calls.Add(1); return "", nil }), ServiceGroups{Sessions: SessionsServices{ListSessions: func() []SessionMeta { return nil }, SessionMessages: func() []DisplayMessage { return nil }, CurrentSessionID: func() string { return "session" }}})
	defer rt.Close()
	if err := rt.EnableEventRecording(root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "session"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "session", "runs"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := rt.StartRun(t.Context(), Input{Text: "run"})
	if !protocol.EvidenceIncomplete(err) || calls.Load() != 0 {
		t.Fatalf("err=%v executions=%d", err, calls.Load())
	}
}
