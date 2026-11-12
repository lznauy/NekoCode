package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"nekocode/mode"
	"nekocode/protocol"
)

func TestBackgroundTerminalIsPublishedOnceAfterWorkerSettles(t *testing.T) {
	for _, tc := range []struct {
		name, status, want              string
		cancel, fail, reportAfterCancel bool
	}{
		{name: "completed", status: "completed", want: "completed"},
		{name: "partial", status: "partial", want: "partial"},
		{name: "failure", status: "failed", want: "failed", fail: true},
		{name: "cancel_after_worker_completion", status: "completed", want: "interrupted", cancel: true},
		{name: "worker_summary_after_cancellation", status: "interrupted", want: "interrupted", cancel: true, reportAfterCancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			services := backgroundServices(t.TempDir())
			services.Modes.Current = func() mode.ID { return mode.Standard }
			rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
			defer rt.Close()
			var count atomic.Int32
			ends := make(chan SubAgentPayload, 4)
			rt.events.AddOrderedObserver(func(e Event) {
				if e.Type == EventSubAgentEnded {
					count.Add(1)
					ends <- e.Payload.(SubAgentPayload)
				}
			})
			reported, release := make(chan struct{}), make(chan struct{})
			_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "task", SessionID: "session", ReadOnly: true, Run: func(ctx context.Context, h protocol.BackgroundHost) (string, error) {
				end := protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentStatus: tc.status, SubAgentAttemptID: "attempt", SubAgentAttemptSeq: 2, Summary: &protocol.RunSummary{StepCount: 7, StopReason: "worker-result"}}
				if tc.reportAfterCancel {
					close(reported)
					<-ctx.Done()
					h.Step(end)
					return "report", ctx.Err()
				}
				h.Step(end)
				close(reported)
				select {
				case <-release:
				case <-ctx.Done():
					return "report", ctx.Err()
				}
				if tc.fail {
					return "report", errors.New("execution failed")
				}
				return "report", nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			awaitBackground(t, reported)
			if n := count.Load(); n != 0 {
				t.Errorf("published %d terminal events before worker returned", n)
			}
			task := waitBackgroundStatus(t, rt, "task", "running")
			if task.Status != "running" {
				t.Error("worker outcome prematurely changed lifecycle")
			}
			if tc.cancel {
				if err := rt.CancelBackgroundTask(t.Context(), "session", "task"); err != nil {
					t.Fatal(err)
				}
			} else {
				close(release)
			}
			select {
			case end := <-ends:
				if end.Status != tc.want || end.AttemptID != "attempt" || end.AttemptSeq != 2 || end.Summary.StepCount != 7 || end.Summary.StopReason != "worker-result" {
					t.Errorf("lost final status, identity or summary: %+v", end)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("missing final event")
			}
			waitBackgroundStatus(t, rt, "task", tc.want)
			rt.Close()
			if n := count.Load(); n != 1 {
				t.Errorf("terminal count=%d, want 1", n)
			}
		})
	}
}
