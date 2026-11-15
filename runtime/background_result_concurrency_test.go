package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"nekocode/protocol"
)

func TestBackgroundResultErrorsSnapshotConcurrentState(t *testing.T) {
	for _, pending := range []bool{true, false} {
		name := "uncommitted"
		if pending {
			name = "pending"
		}
		t.Run(name, func(t *testing.T) {
			rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), backgroundServices(t.TempDir()))
			defer rt.Close()
			b := rt.background
			if err := b.load("session"); err != nil {
				t.Fatal(err)
			}
			status := "failed"
			if pending {
				status = "running"
			}
			b.mu.Lock()
			e := &backgroundEntry{BackgroundTask: BackgroundTask{ID: "task", SessionID: "session", Status: status, ResultVersion: 1, Error: "commit failed"}}
			b.entries[e.ID] = e
			b.order = []string{e.ID}
			b.mu.Unlock()
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 100000 {
					b.mu.Lock()
					if pending {
						e.Status = "waiting_question"
					} else {
						e.Error = "another failure"
					}
					b.mu.Unlock()
					b.mu.Lock()
					e.Status, e.Error = status, "commit failed"
					b.mu.Unlock()
				}
			}()
			defer wg.Wait()
			for range 100000 {
				_, err := rt.readBackgroundResult(t.Context(), "session", protocol.ResultQuery{ID: "task", Limit: 1})
				if pending {
					var notReady *protocol.ResultNotReadyError
					if !errors.As(err, &notReady) || !pendingBackgroundStatus(notReady.Status) {
						t.Fatalf("invalid pending snapshot: %v", err)
					}
				} else if err == nil || !strings.HasPrefix(err.Error(), "background result was not committed: ") {
					t.Fatalf("invalid commit failure snapshot: %v", err)
				}
			}
		})
	}
}
