package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nekocode/mode"
	"nekocode/protocol"
)

// Stall storage at its path boundary rather than relying on disk speed.
func stalledBackgroundStorage(t *testing.T) (ServiceGroups, *atomic.Bool, <-chan struct{}, func()) {
	t.Helper()
	services := backgroundServices(t.TempDir())
	services.Modes.Current = func() mode.ID { return mode.Standard }
	path := services.Sessions.BackgroundStatePath
	var armed atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	services.Sessions.BackgroundStatePath = func(id string) string {
		if armed.CompareAndSwap(true, false) {
			close(entered)
			<-release
		}
		return path(id)
	}
	t.Cleanup(unblock)
	return services, &armed, entered, unblock
}

func TestBackgroundSnapshotWriteDoesNotBlockReadsProgressOrCancellation(t *testing.T) {
	services, armed, entered, unblock := stalledBackgroundStorage(t)
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	defer unblock()
	b := rt.background
	if err := b.load("session"); err != nil {
		t.Fatal(err)
	}
	workerCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	b.mu.Lock()
	b.entries["worker"] = &backgroundEntry{interactionID: "scope", cancel: cancel, BackgroundTask: BackgroundTask{ID: "worker", SessionID: "session", Status: "running"}}
	b.entries["queued"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "queued", SessionID: "session", Status: "queued"}}
	b.order = []string{"worker", "queued"}
	b.mu.Unlock()
	armed.Store(true)
	written := make(chan error, 1)
	go func() { written <- rt.AppendBackgroundTask(t.Context(), "session", "queued", "more detail") }()
	awaitBackground(t, entered)
	responsive := make(chan error, 1)
	go func() {
		tasks, err := rt.BackgroundTasks("session")
		if err != nil {
			responsive <- err
			return
		}
		for _, task := range tasks {
			if task.ID == "queued" && len(task.PendingUpdates) != 0 {
				responsive <- errors.New("an uncommitted update became visible")
				return
			}
		}
		(backgroundHost{runtime: rt, ctx: workerCtx, id: "worker", sessionID: "session", interactionID: "scope"}).Step(protocol.StepEvent{Action: protocol.StepActionSubAgentUpdate})
		responsive <- rt.CancelBackgroundTask(t.Context(), "session", "worker")
	}()
	select {
	case err := <-responsive:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Error("snapshot storage blocked state reads, progress or cancellation")
	}
	if workerCtx.Err() == nil {
		t.Error("running worker did not receive cancellation while storage was stalled")
	}
	unblock()
	if err := <-written; err != nil {
		t.Fatal(err)
	}
}

func TestBackgroundResultCommitDoesNotBlockAnotherWorkerCancellation(t *testing.T) {
	services, armed, entered, unblock := stalledBackgroundStorage(t)
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	defer unblock()
	finish, started, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "finishing", SessionID: "session", ReadOnly: true, Run: func(ctx context.Context, _ protocol.BackgroundHost) (protocol.BackgroundResult, error) {
		select {
		case <-finish:
		case <-ctx.Done():
			return protocol.BackgroundResult{}, ctx.Err()
		}
		return protocol.BackgroundResult{Text: "committed result"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "worker", SessionID: "session", ReadOnly: true, Run: func(ctx context.Context, _ protocol.BackgroundHost) (protocol.BackgroundResult, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return protocol.BackgroundResult{}, ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, started)
	armed.Store(true)
	close(finish)
	awaitBackground(t, entered)
	responsive := make(chan error, 1)
	go func() {
		// Completion already owns this attempt; cancellation cannot rewrite its
		// canonical outcome even while its result file is being committed.
		if err := rt.CancelBackgroundTask(t.Context(), "session", "finishing"); err != nil {
			responsive <- err
			return
		}
		responsive <- rt.CancelBackgroundTask(t.Context(), "session", "worker")
	}()
	select {
	case err := <-responsive:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Error("result commit blocked cancellation of an independent worker")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Error("worker kept running during result commit")
	}
	unblock()
	task := waitBackgroundStatus(t, rt, "finishing", "completed")
	page, err := rt.readBackgroundResult(t.Context(), "session", protocol.ResultQuery{ID: task.ID, Ref: task.ResultRef, Limit: 100})
	if err != nil || page.Content != "committed result" {
		t.Fatalf("lost committed result: %+v %v", page, err)
	}
	waitBackgroundStatus(t, rt, "worker", "interrupted")
}

func TestStopSignalsWorkersBeforeWaitingForSnapshotStorage(t *testing.T) {
	services, armed, entered, unblock := stalledBackgroundStorage(t)
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	defer unblock()
	b := rt.background
	if err := b.load("session"); err != nil {
		t.Fatal(err)
	}
	workerCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	b.mu.Lock()
	b.entries["worker"] = &backgroundEntry{cancel: cancel, BackgroundTask: BackgroundTask{ID: "worker", SessionID: "session", Status: "running"}}
	b.entries["queued"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "queued", SessionID: "session", Status: "queued"}}
	b.order = []string{"worker", "queued"}
	b.mu.Unlock()
	armed.Store(true)
	written := make(chan error, 1)
	go func() { written <- rt.AppendBackgroundTask(t.Context(), "session", "queued", "more detail") }()
	awaitBackground(t, entered)
	stopped := make(chan error, 1)
	go func() { stopped <- rt.StopBackgroundTasks(t.Context(), "session") }()
	select {
	case <-workerCtx.Done():
	case <-time.After(time.Second):
		t.Error("stop waited for storage before signalling the worker")
	}
	unblock()
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	waitBackgroundStatus(t, rt, "queued", "interrupted")
}
