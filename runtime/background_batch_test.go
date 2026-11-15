package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"nekocode/protocol"
)

func decodeBatchNotice(t *testing.T, input string) backgroundReview {
	t.Helper()
	start := strings.Index(input, "\n{")
	if start < 0 {
		t.Fatal("missing review data")
	}
	var review backgroundReview
	if err := json.Unmarshal([]byte(input[start+1:]), &review); err != nil {
		t.Fatal(err)
	}
	return review
}

func TestCleanBatchUsesOneCoordinatorReviewAndUserInputStillRuns(t *testing.T) {
	notices, user := make(chan string, 4), make(chan struct{}, 1)
	rt := New(RunnerFunc(func(ctx context.Context, input string, _ RunHost) (string, error) {
		if protocol.CoordinationFromContext(ctx).Notification {
			notices <- input
		} else {
			user <- struct{}{}
		}
		return "reviewed", nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	a, b := make(chan struct{}), make(chan struct{})
	for _, spec := range []struct {
		id      string
		release chan struct{}
	}{{"a", a}, {"b", b}} {
		_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: spec.id, SessionID: "session", GroupID: "batch", ReviewPolicy: "batch", ReadOnly: true, Run: func(ctx context.Context, host protocol.BackgroundHost) (protocol.BackgroundResult, error) {
			select {
			case <-spec.release:
			case <-ctx.Done():
				return protocol.BackgroundResult{Text: ""}, ctx.Err()
			}
			review := cleanReviewFixture()
			return protocol.BackgroundResult{Review: &review}, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	close(a)
	waitBackgroundStatus(t, rt, "a", "completed")
	rt.background.mu.Lock()
	ids, _ := rt.background.reviewIDsLocked("session")
	rt.background.mu.Unlock()
	if len(ids) != 0 {
		t.Fatal("clean batch completed prematurely")
	}
	run, err := rt.StartRun(t.Context(), Input{Text: "new user demand"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(t.Context(), run); err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, user)
	close(b)
	select {
	case text := <-notices:
		review := decodeBatchNotice(t, text)
		if len(review.Results) != 2 || review.PendingTasks != 0 || review.PendingReviews != 0 {
			t.Fatalf("batch was split or incomplete: %+v", review)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("settled batch did not wake coordinator")
	}
	waitBackgroundStatus(t, rt, "b", "completed")
	// A completed coordinator marks both exact entries consumed before yielding.
	deadline := time.Now().Add(5 * time.Second)
	consumed := false
	for time.Now().Before(deadline) {
		tasks, _ := rt.BackgroundTasks("session")
		if tasks[0].Notified && tasks[1].Notified {
			consumed = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !consumed {
		t.Fatal("batch deliveries were not consumed")
	}
	select {
	case text := <-notices:
		t.Fatalf("redundant coordinator review: %s", text)
	default:
	}
}

func TestBatchFailureFlushesBeforeSlowPeerCompletes(t *testing.T) {
	notices := make(chan string, 4)
	rt := New(RunnerFunc(func(_ context.Context, input string, _ RunHost) (string, error) {
		notices <- input
		return "reviewed", nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "slow", SessionID: "session", GroupID: "batch", ReviewPolicy: "batch", ReadOnly: true, Run: func(ctx context.Context, _ protocol.BackgroundHost) (protocol.BackgroundResult, error) {
		<-ctx.Done()
		return protocol.BackgroundResult{Text: ""}, ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "failed", SessionID: "session", GroupID: "batch", ReviewPolicy: "batch", ReadOnly: true, Run: func(context.Context, protocol.BackgroundHost) (protocol.BackgroundResult, error) {
		return protocol.BackgroundResult{Text: "failure evidence"}, errors.New("regression failed")
	}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case text := <-notices:
		review := decodeBatchNotice(t, text)
		if len(review.Results) != 1 || review.Results[0].Status != "failed" || review.PendingTasks != 1 {
			t.Fatalf("failure delayed or lost: %+v", review)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("failure waited for slow peer")
	}
}

func TestBatchReviewDoesNotBlockDependencyDispatch(t *testing.T) {
	notices, started := make(chan string, 4), make(chan struct{})
	rt := New(RunnerFunc(func(_ context.Context, input string, _ RunHost) (string, error) {
		notices <- input
		return "reviewed", nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	services := rt.background
	// Hold dispatch while both nodes are admitted, so the upstream cannot be
	// delivered before its batch dependent exists.
	rt.mutationMu.Lock()
	_, err := services.submit(t.Context(), protocol.BackgroundWork{ID: "a", SessionID: "session", GroupID: "batch", ReviewPolicy: "batch", ReadOnly: true, Run: func(_ context.Context, h protocol.BackgroundHost) (protocol.BackgroundResult, error) {
		review := cleanReviewFixture()
		return protocol.BackgroundResult{Review: &review}, nil
	}})
	if err != nil {
		rt.mutationMu.Unlock()
		t.Fatal(err)
	}
	_, err = services.submit(t.Context(), protocol.BackgroundWork{ID: "b", SessionID: "session", GroupID: "batch", ReviewPolicy: "batch", After: []string{"a"}, ReadOnly: true, Run: func(_ context.Context, h protocol.BackgroundHost) (protocol.BackgroundResult, error) {
		close(started)
		review := cleanReviewFixture()
		return protocol.BackgroundResult{Review: &review}, nil
	}})
	rt.mutationMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, started)
	select {
	case text := <-notices:
		if len(decodeBatchNotice(t, text).Results) != 2 {
			t.Fatal("dependency batch split")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dependency/review deadlock")
	}
}
