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

// Model the exact crash boundary: the running receipt is durable, the complete
// record has been published, but the final receipt has never been written.
func TestBackgroundRecoveryRebuildsCommittedAttempt(t *testing.T) {
	for _, status := range []string{"completed", "partial", "failed", "interrupted"} {
		t.Run(status, func(t *testing.T) {
			services := backgroundServices(t.TempDir())
			services.Modes.Current = func() mode.ID { return mode.Standard }
			task := BackgroundTask{ID: "task", SessionID: "session", GroupID: "plan", Status: "running", ResultVersion: 1, AttemptID: "attempt", AttemptSeq: 2, Error: "stale recovery diagnostic"}
			statePath := services.Sessions.BackgroundStatePath("session")
			if err := os.MkdirAll(filepath.Dir(statePath), 0700); err != nil {
				t.Fatal(err)
			}
			state, _ := json.Marshal(backgroundState{Version: 2, Tasks: []BackgroundTask{task}})
			if err := os.WriteFile(statePath, state, 0600); err != nil {
				t.Fatal(err)
			}
			report := cleanReviewFixture()
			if status != "completed" {
				report.Outcome = "continue"
				report.NextAction = "inspect remaining work"
				report.Unfinished = []string{"remaining work"}
			}
			expectedError := ""
			if status == "failed" || status == "interrupted" {
				expectedError = "execution diagnostic"
			}
			record, _ := json.Marshal(map[string]any{"session_id": "session", "task_id": "task", "attempt_id": "attempt", "attempt_seq": 2, "status": status, "error": expectedError, "isolation": "worktree", "result": protocol.BackgroundResult{Review: &report}})
			directory := filepath.Join(filepath.Dir(statePath), "background-results", backgroundResultKey("task"))
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, backgroundResultKey("attempt")+".json"), record, 0600); err != nil {
				t.Fatal(err)
			}
			var runs atomic.Int32
			rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { runs.Add(1); return "", nil }), services)
			defer rt.Close()
			tasks, err := rt.BackgroundTasks("session")
			if err != nil || len(tasks) != 1 {
				t.Fatalf("load: %+v %v", tasks, err)
			}
			got := tasks[0]
			if got.Status != status || got.ResultRef == "" || got.Review == nil || got.Review.Summary != report.Summary || got.Error != expectedError || got.AttemptID != "attempt" || got.AttemptSeq != 2 || got.Notified {
				t.Fatalf("committed outcome was not recovered: %+v", got)
			}
			page, err := rt.readBackgroundResult(t.Context(), "session", protocol.ResultQuery{ID: "task", AttemptID: "attempt", Limit: 2000})
			if err != nil || page.Status != status || !strings.Contains(page.Content, report.Summary) {
				t.Fatalf("committed evidence unavailable after restart: %+v %v", page, err)
			}
			if runs.Load() != 0 {
				t.Fatal("recovery replayed execution or notified from inactive mode")
			}
			// Recovery persists the rebuilt projection, so the next restart needs no repair.
			data, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			var saved backgroundState
			if err := json.Unmarshal(data, &saved); err != nil {
				t.Fatal(err)
			}
			if saved.Tasks[0].ResultRef != got.ResultRef || saved.Tasks[0].Status != status {
				t.Fatal("recovered outcome was not persisted")
			}
		})
	}
}

func TestBackgroundAttemptIsDurableBeforePublicWorkerStarts(t *testing.T) {
	services := backgroundServices(t.TempDir())
	services.Modes.Current = func() mode.ID { return mode.Standard }
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	observed := make(chan BackgroundTask, 1)
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "task", SessionID: "session", ReadOnly: true, Run: func(ctx context.Context, host protocol.BackgroundHost) (protocol.BackgroundResult, error) {
		data, err := os.ReadFile(services.Sessions.BackgroundStatePath("session"))
		if err != nil {
			return protocol.BackgroundResult{}, err
		}
		var saved backgroundState
		if err := json.Unmarshal(data, &saved); err != nil {
			return protocol.BackgroundResult{}, err
		}
		// Worker observations must not replace the admitted identity.
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentAttemptID: "different-worker-claim"})
		observed <- saved.Tasks[0]
		if saved.Tasks[0].AttemptID != protocol.ExecutionFromContext(ctx).AttemptID {
			return protocol.BackgroundResult{}, context.Canceled
		}
		return protocol.BackgroundResult{Text: "public host result"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	var expectedAttempt string
	select {
	case task := <-observed:
		expectedAttempt = task.AttemptID
		if task.AttemptID == "" || task.Status != "running" {
			t.Fatalf("worker started before attempt admission was durable: %+v", task)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
	}
	if task := waitBackgroundStatus(t, rt, "task", "completed"); task.AttemptID != expectedAttempt {
		t.Fatalf("worker replaced durable identity: %+v", task)
	}
}

func TestBackgroundRecoveryRequiresMatchingCommittedAttempt(t *testing.T) {
	for _, kind := range []string{"missing", "older_attempt", "corrupt", "foreign_task", "unfinished_complete"} {
		t.Run(kind, func(t *testing.T) {
			services := backgroundServices(t.TempDir())
			services.Modes.Current = func() mode.ID { return mode.Standard }
			path := services.Sessions.BackgroundStatePath("session")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			state, _ := json.Marshal(backgroundState{Version: 2, Tasks: []BackgroundTask{{ID: "task", SessionID: "session", Status: "running", ResultVersion: 1, AttemptID: "current"}}})
			if err := os.WriteFile(path, state, 0600); err != nil {
				t.Fatal(err)
			}
			if kind != "missing" {
				task, attempt := "task", "current"
				if kind == "older_attempt" {
					attempt = "older"
				}
				if kind == "foreign_task" {
					task = "another-task"
				}
				record := backgroundResultRecord{SessionID: "session", TaskID: task, AttemptID: attempt, Status: "completed", Result: protocol.BackgroundResult{Text: "prior evidence"}}
				if kind == "unfinished_complete" {
					report := cleanReviewFixture()
					report.Outcome, report.NextAction, report.Unfinished = "continue", "finish remaining work", []string{"remaining work"}
					record.Result = protocol.BackgroundResult{Review: &report}
				}
				data, _ := json.Marshal(record)
				if kind == "corrupt" {
					data = []byte("broken record")
				}
				directory := filepath.Join(filepath.Dir(path), "background-results", backgroundResultKey("task"))
				if err := os.MkdirAll(directory, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, backgroundResultKey(attempt)+".json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) {
				t.Error("recovery must not execute work")
				return "", nil
			}), services)
			defer rt.Close()
			tasks, err := rt.BackgroundTasks("session")
			if kind == "missing" || kind == "older_attempt" {
				if err != nil || len(tasks) != 1 || tasks[0].Status != "interrupted" || tasks[0].ResultRef != "" {
					t.Fatalf("missing current result borrowed success: %+v %v", tasks, err)
				}
			} else {
				if err == nil {
					t.Fatalf("invalid committed result accepted: %+v", tasks)
				}
				unchanged, readErr := os.ReadFile(path)
				if readErr != nil || string(unchanged) != string(state) {
					t.Fatalf("failed recovery rewrote the receipt: %v", readErr)
				}
			}
		})
	}
}

func TestBackgroundAdmissionSaveFailureRetriesWithoutExecution(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	services := backgroundServices(root)
	services.Modes.Current = func() mode.ID { return mode.Standard }
	goodPath := services.Sessions.BackgroundStatePath("session")
	var failing atomic.Bool
	attempted := make(chan struct{}, 8)
	services.Sessions.BackgroundStatePath = func(string) string {
		if failing.Load() {
			select {
			case attempted <- struct{}{}:
			default:
			}
			return filepath.Join(blocker, "background.json")
		}
		return goodPath
	}
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	var runs atomic.Int32
	rt.mutationMu.Lock()
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "task", SessionID: "session", ReadOnly: true, Run: func(context.Context, protocol.BackgroundHost) (protocol.BackgroundResult, error) {
		runs.Add(1)
		return protocol.BackgroundResult{Text: "once"}, nil
	}})
	if err != nil {
		rt.mutationMu.Unlock()
		t.Fatal(err)
	}
	failing.Store(true)
	rt.mutationMu.Unlock()
	awaitBackground(t, attempted)
	rt.background.mu.Lock()
	task, active := rt.background.entries["task"].BackgroundTask, rt.background.active
	rt.background.mu.Unlock()
	if task.Status != "queued" || task.AttemptID != "" || active != 0 || runs.Load() != 0 {
		t.Fatalf("failed admission started or leaked execution: %+v active=%d runs=%d", task, active, runs.Load())
	}
	failing.Store(false)
	waitBackgroundStatus(t, rt, "task", "completed")
	if runs.Load() != 1 {
		t.Fatalf("retry replayed work: %d", runs.Load())
	}
}
