package runtime

import (
	"context"
	"testing"
	"time"

	"nekocode/mode"
	"nekocode/protocol"
)

func TestQueuedCancellationPublishesTerminalAndPreservesOldAttempt(t *testing.T) {
	for _, stopAll := range []bool{false, true} {
		for _, resumed := range []bool{false, true} {
			t.Run(map[bool]string{false: "cancel", true: "stop_all"}[stopAll]+map[bool]string{false: "_new", true: "_resume"}[resumed], func(t *testing.T) {
				services := backgroundServices(t.TempDir())
				// Keep the coordinator from consuming the cancelled receipt.
				services.Modes.Current = func() mode.ID { return mode.Standard }
				services.Sessions.Subagents = func(string) ([]SubagentSummary, error) {
					if !resumed {
						return nil, nil
					}
					return []SubagentSummary{{ID: "queued", Recorded: true, Status: "completed", RunCount: 1, UpdatedAt: time.Unix(1, 0), LatestAttempt: &SubagentAttemptSummary{ID: "old", Seq: 1, Status: "completed"}}}, nil
				}
				rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
				defer rt.Close()
				rt.events.AddOrderedObserver(func(ev Event) {
					if ev.Type != EventSubAgentEnded {
						return
					}
					if !rt.mutationMu.TryLock() {
						t.Error("terminal event published under mutation lock")
					} else {
						rt.mutationMu.Unlock()
					}
					if !rt.background.mu.TryLock() {
						t.Error("terminal event published under background lock")
					} else {
						rt.background.mu.Unlock()
					}
				})
				rt.background.mu.Lock()
				rt.background.entries["queued"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "queued", SessionID: "session", Status: "queued", Prompt: "new accepted instruction", Title: "new title"}}
				rt.background.order = append(rt.background.order, "queued")
				rt.background.mu.Unlock()
				rt.publishStepForSession("task_queued", protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentID: "queued", SubAgentStatus: "queued"}, "session")
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				events, err := rt.Events(ctx, EventFilter{SubAgentID: "queued", Types: []EventType{EventSubAgentEnded}})
				if err != nil {
					t.Fatal(err)
				}
				if stopAll {
					err = rt.StopBackgroundTasks(ctx, "session")
				} else {
					err = rt.CancelBackgroundTask(ctx, "session", "queued")
				}
				if err != nil {
					t.Fatal(err)
				}
				select {
				case ev := <-events:
					p := ev.Payload.(SubAgentPayload)
					if p.Status != "interrupted" || p.AttemptID != "" {
						t.Fatalf("wrong queued terminal: %+v", p)
					}
				case <-ctx.Done():
					t.Fatal("queued cancellation emitted no terminal event")
				}
				page, err := rt.Subagents(SubagentFilter{SessionID: "session", Limit: 10})
				if err != nil || len(page.Items) != 1 || page.Items[0].Status != "interrupted" {
					t.Fatalf("queued cancellation missing: %+v %v", page, err)
				}
				if resumed && (page.Items[0].LatestAttempt == nil || page.Items[0].LatestAttempt.ID != "old" || page.Items[0].LatestAttempt.Status != "completed") {
					t.Fatalf("cancelled admission altered old attempt: %+v", page.Items[0])
				}
				if page.Items[0].Prompt != "new accepted instruction" || page.Items[0].Title != "new title" {
					t.Fatalf("cancelled admission lost accepted prompt or title: %+v", page.Items[0])
				}
			})
		}
	}
}
