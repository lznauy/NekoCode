package runtime

import (
	"context"
	"errors"
	"fmt"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/mode"
	"nekocode/protocol"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackgroundUpdatesAreTaskAndSessionScoped(t *testing.T) {
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), backgroundServices(t.TempDir()))
	defer rt.Close()
	// No executable closures: exercise admission independently of scheduling.
	b := rt.background
	b.mu.Lock()
	for _, id := range []string{"a", "b"} {
		b.entries[id] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: id, SessionID: "session", Status: "queued"}}
		b.order = append(b.order, id)
	}
	b.mu.Unlock()
	if err := rt.AppendBackgroundTask(t.Context(), "other", "a", "wrong session"); err == nil {
		t.Fatal("cross-session update accepted")
	}
	if err := rt.AppendBackgroundTask(t.Context(), "session", "a", "追加原文\n第二行"); err != nil {
		t.Fatal(err)
	}
	tasks, err := rt.BackgroundTasks("session")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks[0].PendingUpdates) != 1 || len(tasks[1].PendingUpdates) != 0 {
		t.Fatalf("wrong target: %+v", tasks)
	}
	tasks[0].PendingUpdates[0] = "mutated"
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	b.mu.Lock()
	b.entries["a"].cancel = cancel
	b.entries["a"].Status = "running"
	b.mu.Unlock()
	host := backgroundHost{runtime: rt, ctx: ctx, id: "a", sessionID: "session"}
	failure := fmt.Errorf("checkpoint failed")
	if err := host.ApplyUpdates(func([]protocol.BackgroundUpdate) error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("err=%v", err)
	}
	calls := 0
	err = host.ApplyUpdates(func(updates []protocol.BackgroundUpdate) error {
		calls++
		if len(updates) != 1 || updates[0].Text != "追加原文\n第二行" {
			t.Fatalf("updates=%+v", updates)
		}
		// An append during the checkpoint must survive acknowledgement.
		return rt.AppendBackgroundTask(t.Context(), "session", "a", "arrived during checkpoint")
	})
	if err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	if err := host.ApplyUpdates(func(updates []protocol.BackgroundUpdate) error {
		if len(updates) != 1 || updates[0].Text != "arrived during checkpoint" {
			t.Fatalf("wrong remainder: %+v", updates)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := host.ApplyUpdates(func([]protocol.BackgroundUpdate) error { t.Fatal("delivered twice"); return nil }); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err = host.ApplyUpdates(func([]protocol.BackgroundUpdate) error { return nil }); err == nil {
		t.Fatal("cancelled worker read updates")
	}
	b.mu.Lock()
	b.entries["a"].cancel = nil
	b.entries["a"].Status = "completed"
	b.mu.Unlock()
	if err = rt.AppendBackgroundTask(t.Context(), "session", "a", "late"); err == nil {
		t.Fatal("terminal update accepted")
	}
}

func TestBackgroundUpdateBounds(t *testing.T) {
	root := t.TempDir()
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), backgroundServices(root))
	defer rt.Close()
	b := rt.background
	b.mu.Lock()
	b.entries["a"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "a", SessionID: "session", Status: "queued"}}
	b.order = append(b.order, "a")
	b.mu.Unlock()
	if err := rt.AppendBackgroundTask(t.Context(), "session", "a", "  "); err == nil {
		t.Fatal("empty update accepted")
	}
	for range 16 {
		if err := rt.AppendBackgroundTask(t.Context(), "session", "a", "bounded"); err != nil {
			t.Fatal(err)
		}
	}
	if err := rt.AppendBackgroundTask(t.Context(), "session", "a", "overflow"); err == nil {
		t.Fatal("unbounded queue")
	}
}

func TestBackgroundUpdatePersistenceRollback(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "session")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), backgroundServices(root))
	defer rt.Close()
	b := rt.background
	b.mu.Lock()
	b.entries["a"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "a", SessionID: "session", Status: "queued"}}
	b.order = append(b.order, "a")
	b.mu.Unlock()
	if err := rt.AppendBackgroundTask(t.Context(), "session", "a", "not accepted"); err == nil {
		t.Fatal("storage failure ignored")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.entries["a"].PendingUpdates) != 0 {
		t.Fatal("failed admission retained")
	}
}

func TestBackgroundResumePreservesUnreadUpdates(t *testing.T) {
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), backgroundServices(t.TempDir()))
	defer rt.Close()
	b := rt.background
	b.mu.Lock()
	b.entries["a"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "a", SessionID: "session", Status: "completed", Notified: true, PendingUpdates: []string{"unread at completion"}}}
	b.order = append(b.order, "a")
	b.mu.Unlock()
	delivered := make(chan struct{})
	_, err := b.submit(t.Context(), protocol.BackgroundWork{ID: "a", SessionID: "session", ReadOnly: true, Run: func(ctx context.Context, host protocol.BackgroundHost) (string, error) {
		err := host.(protocol.BackgroundInputSource).ApplyUpdates(func(updates []protocol.BackgroundUpdate) error {
			if len(updates) != 1 || updates[0].Text != "unread at completion" {
				t.Errorf("lost unread update: %+v", updates)
			}
			return nil
		})
		close(delivered)
		return "", err
	}})
	if err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, delivered)
}

func TestUpdateSequenceUsesCanonicalFloorWhenRuntimeHistoryWasPruned(t *testing.T) {
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), backgroundServices(t.TempDir()))
	defer rt.Close()
	started, release, delivered := make(chan struct{}), make(chan struct{}), make(chan struct{})
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "pruned", SessionID: "session", ReadOnly: true, UpdateSequenceFloor: 50, Run: func(ctx context.Context, host protocol.BackgroundHost) (string, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		err := host.(protocol.BackgroundInputSource).ApplyUpdates(func(updates []protocol.BackgroundUpdate) error {
			if len(updates) != 1 || updates[0].Sequence != 51 {
				t.Errorf("reused old sequence: %+v", updates)
			}
			return nil
		})
		close(delivered)
		return "", err
	}})
	if err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, started)
	if err := rt.AppendBackgroundTask(t.Context(), "session", "pruned", "new instruction"); err != nil {
		t.Fatal(err)
	}
	close(release)
	awaitBackground(t, delivered)
}

func TestResumedUpdateSequenceReconcilesRetainedHistoryAndCanonicalReceipt(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		oldSequence, canonical uint64
		pending                []string
		wantNew                uint64
		wantMessages           int
	}{
		{name: "lagging_history", oldSequence: 5, canonical: 50, wantNew: 51, wantMessages: 1},
		{name: "lagging_history_with_consumed_prefix", oldSequence: 5, canonical: 50, pending: []string{"old one", "old two"}, wantNew: 51, wantMessages: 1},
		{name: "checkpointed_but_unacknowledged", oldSequence: 50, canonical: 50, pending: []string{"already checkpointed"}, wantNew: 51, wantMessages: 1},
		{name: "unread_updates_ahead_of_receipt", oldSequence: 52, canonical: 50, pending: []string{"unread one", "unread two"}, wantNew: 53, wantMessages: 3},
		{name: "legacy_missing_sequence", pending: []string{"legacy one", "legacy two"}, wantNew: 3, wantMessages: 3},
		{name: "sequence_exhaustion_boundary", oldSequence: 5, canonical: ^uint64(0) - 1, wantNew: ^uint64(0), wantMessages: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			services := backgroundServices(t.TempDir())
			services.Modes.Current = func() mode.ID { return mode.Standard }
			runner := RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil })
			original := New(runner, services)
			if err := original.background.load("session"); err != nil {
				original.Close()
				t.Fatal(err)
			}
			b := original.background
			b.mu.Lock()
			b.entries["resume"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "resume", SessionID: "session", Status: "interrupted", Notified: true, UpdateSequence: tc.oldSequence, PendingUpdates: tc.pending}}
			b.order = []string{"resume"}
			err := b.saveLocked("session")
			b.mu.Unlock()
			original.Close()
			if err != nil {
				t.Fatal(err)
			}
			recovered := New(runner, services)
			defer recovered.Close()
			started, release := make(chan struct{}), make(chan struct{})
			type observed struct {
				snapshot ctxmgr.ManagerSnapshot
				updates  []protocol.BackgroundUpdate
				err      error
			}
			received := make(chan observed, 1)
			_, err = recovered.background.submit(t.Context(), protocol.BackgroundWork{ID: "resume", SessionID: "session", ReadOnly: true, UpdateSequenceFloor: tc.canonical, Run: func(ctx context.Context, host protocol.BackgroundHost) (string, error) {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
					return "", ctx.Err()
				}
				manager := ctxmgr.New(ctxmgr.Config{})
				manager.Restore(ctxmgr.ManagerSnapshot{TaskUpdateSequence: tc.canonical})
				var applied []protocol.BackgroundUpdate
				applyErr := host.(protocol.BackgroundInputSource).ApplyUpdates(func(updates []protocol.BackgroundUpdate) error {
					applied = append(applied, updates...)
					for _, u := range updates {
						manager.AddTaskUpdate(u.Sequence, u.Text)
					}
					return nil
				})
				received <- observed{manager.Snapshot(), applied, applyErr}
				return "done", applyErr
			}})
			if err != nil {
				t.Fatal(err)
			}
			awaitBackground(t, started)
			if err := recovered.AppendBackgroundTask(t.Context(), "session", "resume", "brand-new instruction"); err != nil {
				t.Fatal(err)
			}
			if tc.wantNew == ^uint64(0) {
				if err := recovered.AppendBackgroundTask(t.Context(), "session", "resume", "must not wrap around"); err == nil {
					t.Error("accepted an update after sequence exhaustion")
				}
			}
			close(release)
			var got observed
			select {
			case got = <-received:
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not apply updates")
			}
			if got.err != nil {
				t.Fatal(got.err)
			}
			if len(got.updates) == 0 || got.updates[len(got.updates)-1].Sequence != tc.wantNew {
				t.Errorf("new update sequence: %+v, want %d", got.updates, tc.wantNew)
			}
			messages := got.snapshot.Messages
			if len(messages) != tc.wantMessages || len(messages) == 0 || messages[len(messages)-1].Content != "brand-new instruction" {
				t.Errorf("new instruction lost or old updates replayed: %+v", messages)
			}
		})
	}
}
