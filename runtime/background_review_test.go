package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"nekocode/mode"
	"nekocode/protocol"
)

func TestBackgroundReviewBudgetMarksMissingContentAndKeepsInventory(t *testing.T) {
	b := &backgroundTasks{entries: map[string]*backgroundEntry{}}
	body := strings.Repeat("证据🙂", 3000)
	for _, id := range []string{"A", "B", "C", "previous", "pending", "other"} {
		e := &backgroundEntry{BackgroundTask: BackgroundTask{ID: id, SessionID: "session", GroupID: "group", Status: "completed", Result: body, AttemptID: "attempt-" + id}}
		if id == "previous" {
			e.Notified, e.Delivery = true, "delivered"
		}
		if id == "pending" {
			e.Status = "waiting_dependencies"
		}
		if id == "other" {
			e.GroupID = "different-demand"
		}
		b.entries[id] = e
		b.order = append(b.order, id)
	}
	review := b.reviewLocked("session", "group", []string{"A", "B", "C"})
	if review.PendingTasks != 1 || len(review.GroupTasks) != 5 {
		t.Fatalf("lost task inventory: %+v", review)
	}
	total := 0
	for _, result := range review.Results {
		total += len(result.Result)
		if !utf8.ValidString(result.Result) {
			t.Fatal("split a report inside a Unicode character")
		}
		if result.ResultComplete && result.Result != body {
			t.Fatal("marked truncated content complete")
		}
		if !result.ResultComplete && (result.ReadResult == nil || result.ReadResult.AttemptID != result.AttemptID) {
			t.Fatal("missing pinned first-page request for incomplete report")
		}
	}
	if total > backgroundReviewResultBudget || review.Results[2].ResultComplete {
		t.Fatal("combined report budget was ignored")
	}
	if b.entries["C"].Result != body {
		t.Fatal("handoff shortened persisted report")
	}
}

func TestBackgroundStoredTruncationSurvivesRecoveryAndRequiresFullRead(t *testing.T) {
	services := backgroundServices(t.TempDir())
	services.Modes.Current = func() mode.ID { return mode.Standard }
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	body := "x" + strings.Repeat("证据🙂", backgroundResultLimit/8)
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "large", SessionID: "session", GroupID: "group", Run: func(context.Context, protocol.BackgroundHost) (string, error) { return body, nil }})
	if err != nil {
		rt.Close()
		t.Fatal(err)
	}
	waitBackgroundStatus(t, rt, "large", "completed")
	rt.Close()
	recovered := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer recovered.Close()
	if err := recovered.background.load("session"); err != nil {
		t.Fatal(err)
	}
	b := recovered.background
	b.mu.Lock()
	review := b.reviewLocked("session", "group", []string{"large"})
	b.mu.Unlock()
	result := review.Results[0]
	if !result.ResultTruncated || result.ResultComplete || result.ReadResult == nil || !utf8.ValidString(result.Result) {
		t.Fatalf("stored preview masqueraded as complete report: complete=%v truncated=%v", result.ResultComplete, result.ResultTruncated)
	}
}

func TestBackgroundReviewKeepsUngroupedAndGroupedDemandReportsSeparate(t *testing.T) {
	notices := make(chan string, 2)
	rt := New(RunnerFunc(func(_ context.Context, input string, _ RunHost) (string, error) {
		notices <- input
		return "reviewed", nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	b := rt.background
	b.mu.Lock()
	b.entries["A"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "A", SessionID: "session", Status: "completed", Result: "first demand"}}
	b.entries["B"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "B", SessionID: "session", GroupID: "second-demand", Status: "completed", Result: "second demand"}}
	b.order = []string{"A", "B"}
	b.mu.Unlock()
	b.signal()
	for _, want := range []string{"A", "B"} {
		select {
		case input := <-notices:
			start := strings.Index(input, "\n{")
			if start < 0 {
				t.Fatal("missing review inventory")
			}
			var review backgroundReview
			if err := json.Unmarshal([]byte(input[start+1:]), &review); err != nil {
				t.Fatal(err)
			}
			if len(review.Results) != 1 || review.Results[0].ID != want || len(review.GroupTasks) != 1 || review.GroupTasks[0].ID != want {
				t.Fatalf("mixed unrelated demands: %+v", review)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("missing demand review")
		}
	}
}

func TestBackgroundTextPrefixBounds(t *testing.T) {
	for _, limit := range []int{-10, -1, 0, 1, 2, 3, 4, 99} {
		got := backgroundTextPrefix("证据🙂", limit)
		if limit <= 0 && got != "" {
			t.Fatalf("limit %d returned %q", limit, got)
		}
		if limit > 0 && len(got) > limit {
			t.Fatalf("limit %d returned %d bytes", limit, len(got))
		}
		if !utf8.ValidString(got) {
			t.Fatalf("limit %d split UTF-8", limit)
		}
	}
}
