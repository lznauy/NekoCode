package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nekocode/mode"
	"nekocode/protocol"
)

func TestBackgroundMutatorsLoadColdSession(t *testing.T) {
	for _, operation := range []string{"cancel", "append"} {
		for _, parent := range []string{"session", "other"} {
			t.Run(operation+"/"+parent, func(t *testing.T) {
				services := backgroundServices(t.TempDir())
				services.Modes.Current = func() mode.ID { return mode.Standard }
				path := services.Sessions.BackgroundStatePath(parent)
				state := backgroundState{Version: 2, Tasks: []BackgroundTask{
					{ID: "saved", SessionID: parent, Status: "queued", PendingUpdates: []string{"retained guidance"}, UpdateSequence: 1},
					{ID: "history", SessionID: parent, Status: "completed", Notified: true, Result: "retained result"},
				}}
				data, err := json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
				defer rt.Close()
				if operation == "cancel" {
					if err := rt.CancelBackgroundTask(t.Context(), parent, "saved"); err != nil {
						t.Fatalf("cold cancel: %v", err)
					}
				} else {
					err := rt.AppendBackgroundTask(t.Context(), parent, "saved", "new guidance")
					// A restarted worker is interrupted, not silently replayed.
					if err == nil || !strings.Contains(err.Error(), "resume it explicitly") {
						t.Fatalf("cold append did not identify recovered task: %v", err)
					}
				}
				rt.background.mu.Lock()
				loaded := rt.background.loaded[parent]
				rt.background.mu.Unlock()
				if !loaded {
					t.Fatal("mutator did not load target session")
				}
				data, err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var saved backgroundState
				if err := json.Unmarshal(data, &saved); err != nil {
					t.Fatal(err)
				}
				if len(saved.Tasks) != 2 || saved.Tasks[0].Status != "interrupted" || saved.Tasks[0].PendingUpdates[0] != "retained guidance" || saved.Tasks[1].Result != "retained result" {
					t.Fatalf("recovery lost session state: %+v", saved.Tasks)
				}
			})
		}
	}
}

func TestBackgroundMutatorsPropagateLoadFailure(t *testing.T) {
	for _, operation := range []string{"cancel", "append"} {
		t.Run(operation, func(t *testing.T) {
			services := backgroundServices(t.TempDir())
			path := services.Sessions.BackgroundStatePath("other")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			original := []byte("invalid background state")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
			defer rt.Close()
			var err error
			if operation == "cancel" {
				err = rt.CancelBackgroundTask(t.Context(), "other", "saved")
			} else {
				err = rt.AppendBackgroundTask(t.Context(), "other", "saved", "guidance")
			}
			if err == nil || !strings.Contains(err.Error(), "read background state") {
				t.Fatalf("load error was hidden: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != string(original) {
				t.Fatalf("failed load rewrote state: %q %v", data, err)
			}
		})
	}
}

func TestDependencySaveFailurePreservesExecutionAndDiagnostic(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	services := backgroundServices(root)
	services.Sessions.BackgroundStatePath = func(string) string { return filepath.Join(blocker, "background.json") }
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	var released atomic.Int32
	b := rt.background
	b.mu.Lock()
	b.entries["upstream"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "upstream", SessionID: "session", Status: "failed", Notified: true}}
	e := &backgroundEntry{BackgroundTask: BackgroundTask{ID: "dependent", SessionID: "session", Status: "waiting_dependencies", Error: "previous diagnostic", After: []string{"upstream"}}, work: protocol.BackgroundWork{Run: func(context.Context, protocol.BackgroundHost) (string, error) { return "", nil }, Release: func() { released.Add(1) }}}
	b.entries[e.ID] = e
	b.order = []string{"upstream", e.ID}
	b.resolveDependenciesLocked()
	status, diagnostic, hasRun, hasRelease := e.Status, e.Error, e.work.Run != nil, e.work.Release != nil
	b.mu.Unlock()
	if status != "waiting_dependencies" || diagnostic != "previous diagnostic" || !hasRun || !hasRelease || released.Load() != 0 {
		t.Fatalf("failed persistence changed execution state: status=%s diagnostic=%q run=%v release=%v released=%d", status, diagnostic, hasRun, hasRelease, released.Load())
	}
}

func TestDependencyPersistenceRecoversWithoutAnotherWake(t *testing.T) {
	for _, upstreamStatus := range []string{"completed", "failed"} {
		t.Run(upstreamStatus, func(t *testing.T) {
			root := t.TempDir()
			blocker := filepath.Join(root, "blocker")
			if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
				t.Fatal(err)
			}
			var failing atomic.Bool
			failing.Store(true)
			attempt := make(chan struct{}, 8)
			services := backgroundServices(root)
			services.Modes.Current = func() mode.ID { return mode.Standard }
			goodPath := services.Sessions.BackgroundStatePath("session")
			services.Sessions.BackgroundStatePath = func(string) string {
				if failing.Load() {
					select {
					case attempt <- struct{}{}:
					default:
					}
					return filepath.Join(blocker, "background.json")
				}
				return goodPath
			}
			rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
			defer rt.Close()
			var ran, released atomic.Int32
			b := rt.background
			b.mu.Lock()
			b.loaded["session"] = true
			b.entries["upstream"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "upstream", SessionID: "session", Status: upstreamStatus, Notified: true}}
			e := &backgroundEntry{interactionID: "task_scope_recovery", BackgroundTask: BackgroundTask{ID: "dependent", SessionID: "session", Status: "waiting_dependencies", After: []string{"upstream"}}, work: protocol.BackgroundWork{ReadOnly: true, Run: func(context.Context, protocol.BackgroundHost) (string, error) { ran.Add(1); return "done", nil }, Release: func() { released.Add(1) }}}
			b.entries[e.ID] = e
			b.order = []string{"upstream", e.ID}
			b.mu.Unlock()
			b.signal()
			awaitBackground(t, attempt)
			// Wait for the failed transaction to finish before restoring storage.
			b.mu.Lock()
			status := e.Status
			b.mu.Unlock()
			if status != "waiting_dependencies" {
				t.Fatal(status)
			}
			failing.Store(false)
			want := "completed"
			if upstreamStatus == "failed" {
				want = "blocked"
			}
			waitBackgroundStatus(t, rt, e.ID, want)
			wantRuns := int32(1)
			if upstreamStatus == "failed" {
				wantRuns = 0
			}
			if ran.Load() != wantRuns || released.Load() != 1 {
				t.Fatalf("retry replayed or leaked worker: runs=%d releases=%d", ran.Load(), released.Load())
			}
			data, err := os.ReadFile(goodPath)
			if err != nil {
				t.Fatal(err)
			}
			var saved backgroundState
			if err = json.Unmarshal(data, &saved); err != nil || len(saved.Tasks) != 2 || saved.Tasks[1].Status != want {
				t.Fatalf("recovered state not durable: %+v %v", saved, err)
			}
			if err = rt.Close(); err != nil {
				t.Fatal(err)
			}
			if released.Load() != 1 {
				t.Fatal("shutdown released worker twice")
			}
		})
	}
}

func TestDependencyRetryShutdownReleasesWithoutExecuting(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	attempts := make(chan struct{}, 8)
	services := backgroundServices(root)
	services.Modes.Current = func() mode.ID { return mode.Standard }
	services.Sessions.BackgroundStatePath = func(string) string {
		select {
		case attempts <- struct{}{}:
		default:
		}
		return filepath.Join(blocker, "background.json")
	}
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	var ran, released atomic.Int32
	b := rt.background
	b.mu.Lock()
	b.loaded["session"] = true
	b.entries["upstream"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "upstream", SessionID: "session", Status: "failed", Notified: true}}
	e := &backgroundEntry{BackgroundTask: BackgroundTask{ID: "dependent", SessionID: "session", Status: "waiting_dependencies", After: []string{"upstream"}}, work: protocol.BackgroundWork{Run: func(context.Context, protocol.BackgroundHost) (string, error) { ran.Add(1); return "", nil }, Release: func() { released.Add(1) }}}
	b.entries[e.ID] = e
	b.order = []string{"upstream", e.ID}
	b.mu.Unlock()
	b.signal()
	awaitBackground(t, attempts)
	awaitBackground(t, attempts)
	closed := make(chan struct{})
	go func() { rt.Close(); close(closed) }()
	awaitBackground(t, closed)
	if ran.Load() != 0 || released.Load() != 1 {
		t.Fatalf("shutdown ran or double-released pending task: runs=%d releases=%d", ran.Load(), released.Load())
	}
}

func TestResumedBackgroundTaskKeepsDemandAcrossRestart(t *testing.T) {
	services := backgroundServices(t.TempDir())
	path := services.Sessions.BackgroundStatePath("session")
	state := backgroundState{Version: 2, Tasks: []BackgroundTask{
		{ID: "saved", SessionID: "session", GroupID: "original", Status: "completed", Notified: true, Delivery: "delivered"},
		{ID: "other", SessionID: "session", GroupID: "unrelated", Status: "completed", Notified: true, Delivery: "delivered"},
	}}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	delivered := make(chan string, 1)
	rt := New(RunnerFunc(func(ctx context.Context, _ string, _ RunHost) (string, error) {
		delivered <- protocol.CoordinationFromContext(ctx).GroupID
		return "reviewed", nil
	}), services)
	defer rt.Close()
	foreignCtx := protocol.WithCoordination(t.Context(), protocol.Coordination{GroupID: "unrelated", Notification: true})
	if _, err := rt.background.submit(foreignCtx, protocol.BackgroundWork{ID: "saved", SessionID: "session", GroupID: "unrelated", Run: func(context.Context, protocol.BackgroundHost) (string, error) { return "", nil }}); err == nil {
		t.Fatal("foreign completion notification resumed a legacy task")
	}
	workerGroup := make(chan string, 1)
	receipt, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "saved", SessionID: "session", GroupID: "new-run", ReadOnly: true, Run: func(ctx context.Context, _ protocol.BackgroundHost) (string, error) {
		workerGroup <- protocol.CoordinationFromContext(ctx).GroupID
		return "done", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.GroupID != "original" {
		t.Fatalf("receipt changed demand: %+v", receipt)
	}
	select {
	case group := <-workerGroup:
		if group != "original" {
			t.Fatalf("worker changed demand: %s", group)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
	}
	select {
	case group := <-delivered:
		if group != "original" {
			t.Fatalf("notification changed demand: %s", group)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("notification was not delivered")
	}
	rt.background.mu.Lock()
	defer rt.background.mu.Unlock()
	if rt.background.entries["other"].GroupID != "unrelated" {
		t.Fatal("resume modified unrelated demand")
	}
}
