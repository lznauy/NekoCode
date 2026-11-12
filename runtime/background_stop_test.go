package runtime

import (
	"context"
	"nekocode/mode"
	"nekocode/protocol"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStopBackgroundTasksStopsWorkersAndQueuedDemands(t *testing.T) {
	started, finished := make(chan struct{}, 2), make(chan struct{}, 2)
	var reviews, queuedRuns atomic.Int32
	rt := New(RunnerFunc(func(ctx context.Context, input string, host RunHost) (string, error) {
		if protocol.CoordinationFromContext(ctx).Notification {
			reviews.Add(1)
			return "", nil
		}
		if input == "queued" {
			queuedRuns.Add(1)
			return "", nil
		}
		if input == "fresh" {
			return "fresh accepted", nil
		}
		for _, id := range []string{"a", "b"} {
			_, err := host.(protocol.BackgroundSubmitter).SubmitBackground(ctx, protocol.BackgroundWork{ID: id, SessionID: "session", ReadOnly: true, Run: func(ctx context.Context, _ protocol.BackgroundHost) (string, error) {
				started <- struct{}{}
				<-ctx.Done()
				finished <- struct{}{}
				return "", ctx.Err()
			}})
			if err != nil {
				return "", err
			}
		}
		<-ctx.Done()
		return "", ctx.Err()
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	run, err := rt.StartRun(ctx, Input{Text: "spawn"})
	if err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, started)
	awaitBackground(t, started)
	queued, err := rt.StartRun(ctx, Input{Text: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.StopBackgroundTasks(ctx, "session"); err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(ctx, queued); err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, finished)
	awaitBackground(t, finished)
	tasks, err := rt.BackgroundTasks("session")
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if !task.Notified || task.Delivery != "stopped" {
			t.Fatalf("review still eligible: %+v", task)
		}
	}
	if reviews.Load() != 0 || queuedRuns.Load() != 0 {
		t.Fatal("stopped work was restarted")
	}
	fresh, err := rt.StartRun(ctx, Input{Text: "fresh"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(ctx, fresh); err != nil {
		t.Fatal(err)
	}
}

func TestStopBackgroundTasksKeepsOtherSessionAndReservedTurnCancelled(t *testing.T) {
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) {
		t.Error("reserved turn restarted")
		return "", nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	b := rt.background
	reserved, cancel := context.WithCancel(t.Context())
	foreign, cancelForeign := context.WithCancel(t.Context())
	defer cancelForeign()
	b.mu.Lock()
	b.coordinatorSession, b.coordinatorCancel = "session", cancel
	b.entries["foreign"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "foreign", SessionID: "other", Status: "running"}, cancel: cancelForeign}
	b.order = append(b.order, "foreign")
	b.mu.Unlock()
	if err := rt.StopBackgroundTasks(t.Context(), "session"); err != nil {
		t.Fatal(err)
	}
	if reserved.Err() == nil || foreign.Err() != nil {
		t.Fatal("wrong cancellation scope")
	}
	if _, err := rt.StartRun(reserved, Input{Text: "reserved review"}); err == nil {
		t.Fatal("cancelled reservation admitted")
	}
}

func TestBackgroundDemandRejectsChangedSession(t *testing.T) {
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { t.Error("wrong session ran"); return "", nil }), backgroundServices(t.TempDir()))
	defer rt.Close()
	if _, err := rt.SubmitBackgroundDemand(t.Context(), "stale", Input{Text: "keep draft"}); err == nil {
		t.Fatal("stale session admitted")
	}
}

func TestStopReportsSuppressedUnconsumedResults(t *testing.T) {
	services := backgroundServices(t.TempDir())
	services.Modes.Current = func() mode.ID { return mode.Standard }
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	events, err := rt.Events(t.Context(), EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	b := rt.background
	b.mu.Lock()
	for _, task := range []BackgroundTask{
		{ID: "unread", SessionID: "session", Status: "completed", Result: "retained evidence"},
		{ID: "consumed", SessionID: "session", Status: "completed", Notified: true, Delivery: "delivered"},
		{ID: "other", SessionID: "other", Status: "completed"},
	} {
		b.entries[task.ID] = &backgroundEntry{BackgroundTask: task}
		b.order = append(b.order, task.ID)
	}
	b.mu.Unlock()
	if err = rt.StopBackgroundTasks(t.Context(), "session"); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case ev := <-events:
			if ev.Type != EventSystemMessage {
				continue
			}
			payload, ok := ev.Payload.(MessagePayload)
			if !ok || !strings.Contains(payload.Content, "1 项已结束但尚未消费") {
				t.Fatalf("incorrect suppression notice: %+v", ev)
			}
			if b.entries["unread"].Result != "retained evidence" || b.entries["other"].Notified {
				t.Fatal("result removed or another session stopped")
			}
			return
		case <-time.After(time.Second):
			t.Fatal("suppression notice missing")
		}
	}
}

func TestStopActiveQueuedDemandReleasesItsPendingLifecycle(t *testing.T) {
	firstStarted, releaseFirst, queuedStarted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	rt := New(RunnerFunc(func(ctx context.Context, input string, _ RunHost) (string, error) {
		switch input {
		case "first":
			close(firstStarted)
			select {
			case <-releaseFirst:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		case "queued":
			close(queuedStarted)
			<-ctx.Done()
			return "", ctx.Err()
		}
		return input, nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	first, err := rt.StartRun(ctx, Input{Text: "first"})
	if err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, firstStarted)
	queued, err := rt.StartRun(ctx, Input{Text: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	close(releaseFirst)
	awaitBackground(t, queuedStarted)
	// Queued IDs are real run identities once admitted, not a disjoint namespace.
	rt.mu.Lock()
	current := rt.currentRun
	rt.mu.Unlock()
	if current != queued {
		t.Fatalf("queued demand did not become current: current=%s queued=%s", current, queued)
	}
	if err := rt.StopBackgroundTasks(ctx, "session"); err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(ctx, queued); err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(ctx, first); err != nil {
		t.Fatal(err)
	}
	// WaitRun observes execution termination; the coordinator owns final pending
	// cleanup, so wait for its receipt rather than asserting immediately on done.
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		rt.background.mu.Lock()
		remaining := len(rt.background.pending)
		rt.background.mu.Unlock()
		if remaining == 0 {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("stopped queued demand retained pending lifecycle")
		}
	}
	fresh, err := rt.StartRun(ctx, Input{Text: "fresh"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(ctx, fresh); err != nil {
		t.Fatal(err)
	}
}
