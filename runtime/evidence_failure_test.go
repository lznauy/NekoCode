package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"nekocode/protocol"
)

func TestIncompleteEvidenceSurvivesFailureCancellationAndClose(t *testing.T) {
	for _, mode := range []string{"failure", "cancel", "close", "ordinary"} {
		t.Run(mode, func(t *testing.T) {
			started := make(chan struct{})
			cause := errors.New("history write failed")
			rt := New(RunnerFunc(func(ctx context.Context, _ string, host RunHost) (string, error) {
				host.Step(protocol.StepEvent{Action: protocol.StepActionExecuteTool, CallID: "once", ToolName: "write", Output: "already executed"})
				close(started)
				if mode == "cancel" || mode == "close" {
					<-ctx.Done()
				}
				if mode == "ordinary" {
					return "partial", cause
				}
				return "partial", errors.Join(ctx.Err(), protocol.MarkEvidenceIncomplete(cause))
			}), ServiceGroups{})
			defer rt.Close()
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
				if err := rt.Close(); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if err := rt.WaitRun(ctx, id); err != nil {
				t.Fatal(err)
			}
			snapshot, ok := rt.LookupRun(id)
			if !ok || snapshot.EvidenceIncomplete != (mode != "ordinary") {
				t.Fatalf("snapshot=%+v", snapshot)
			}
			completions, terminals := 0, 0
			for _, event := range rt.events.History(EventFilter{RunID: id}) {
				if event.Type == EventToolCompleted {
					completions++
				}
				switch event.Type {
				case EventRunFailed, EventRunCancelled:
					terminals++
					result := event.Payload.(RunResult)
					if result.EvidenceIncomplete != (mode != "ordinary") {
						t.Fatalf("terminal=%+v", result)
					}
					if mode == "failure" && result.Output != "partial" {
						t.Fatal("save failure lost returned output")
					}
				}
			}
			if completions != 1 || terminals != 1 {
				t.Fatalf("executions=%d terminals=%d", completions, terminals)
			}
		})
	}
}
