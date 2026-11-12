package runtime

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCurrentRunExistsThroughoutAttachmentPreparation(t *testing.T) {
	for _, outcome := range []string{"completed", "failed", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			var block atomic.Bool
			var once sync.Once
			var started atomic.Int32
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			services := ServiceGroups{}
			services.Models.ImageAttachmentsEnabled = func() bool { return true }
			services.Sessions.ListSessions = func() []SessionMeta { return nil }
			services.Sessions.SessionMessages = func() []DisplayMessage { return nil }
			services.Sessions.CurrentSessionID = func() string {
				if block.Load() {
					once.Do(func() { close(entered) })
					<-release
				}
				return "session"
			}
			rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "done", nil }), services)
			defer rt.Close()
			defer releaseOnce.Do(func() { close(release) })
			rt.events.AddOrderedObserver(func(e Event) {
				if e.Type == EventRunStarted {
					started.Add(1)
				}
			})
			first, err := rt.StartRun(t.Context(), Input{Text: "first"})
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.WaitRun(t.Context(), first); err != nil {
				t.Fatal(err)
			}
			block.Store(true)
			path := ""
			if outcome == "failed" {
				path = t.TempDir()
			}
			second, err := rt.StartRun(t.Context(), Input{Source: SourceRef{Kind: "gui", ID: "client"}, Text: "new image draft", Images: []ImageAttachment{{Label: "image", Path: path}}})
			if err != nil {
				t.Fatal(err)
			}
			awaitBackground(t, entered)
			current, ok := rt.CurrentRun()
			if !ok || current.ID != second || current.Status != RunRunning || current.Source.Kind != "gui" || current.Source.ID != "client" {
				t.Errorf("missing or wrong preparing run: %+v %v", current, ok)
			}
			if snapshot, ok := rt.LookupRun(second); !ok || snapshot.EventCount != 0 || snapshot.Input != "" || snapshot.StartedAt.IsZero() {
				t.Errorf("preparation snapshot fabricated acceptance or missing: %+v %v", snapshot, ok)
			}
			if started.Load() != 1 {
				t.Error("published run_started before attachment preparation finished")
			}
			if outcome == "cancelled" {
				if err := rt.CancelRun(t.Context(), second); err != nil {
					t.Fatal(err)
				}
			}
			releaseOnce.Do(func() { close(release) })
			if err := rt.WaitRun(t.Context(), second); err != nil {
				t.Fatal(err)
			}
			final, ok := rt.CurrentRun()
			want := RunDone
			if outcome == "failed" {
				want = RunFailed
			}
			if outcome == "cancelled" {
				want = RunCancelled
			}
			if !ok || final.ID != second || final.Status != want {
				t.Errorf("wrong settled run: %+v %v", final, ok)
			}
		})
	}
}
