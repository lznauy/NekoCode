package task

import (
	"context"
	"encoding/json"
	"fmt"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"strings"
	"testing"
)

func TestShortCursorPinsEveryFieldAndReauthorizesAfterRebind(t *testing.T) {
	tool := NewResultTool()
	long := strings.Repeat("opaque-exact-identity", 30)
	seen := 0
	reader := func(_ context.Context, q taskbridge.ResultQuery) (taskbridge.ResultPage, error) {
		seen++
		if q.Offset > 0 && (q.ID != "task" || q.AttemptID != "attempt" || q.Ref != long || q.Offset != 2000 || q.Limit != 2000) {
			t.Fatalf("cursor altered pinned query: %+v", q)
		}
		return taskbridge.ResultPage{ID: "task", AttemptID: "attempt", Ref: long, Offset: q.Offset, NextOffset: 2000, Content: "evidence", HasMore: q.Offset == 0}, nil
	}
	tool.Wire(reader)
	first, err := tool.Execute(t.Context(), map[string]any{"id": "task", "limit": float64(2000)})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		NextCall map[string]any `json:"next_call"`
	}
	json.Unmarshal([]byte(first.Text), &result)
	cursor, ok := result.NextCall["cursor"].(string)
	if !ok || len(cursor) > 40 || len(result.NextCall) != 1 {
		t.Fatalf("not a short cursor: %+v", result.NextCall)
	}
	tool.Wire(reader)
	if _, err = tool.Execute(t.Context(), result.NextCall); err != nil {
		t.Fatal(err)
	}
	if seen != 2 {
		t.Fatal("underlying reader not invoked for continuation")
	}
	for _, args := range []map[string]any{{"cursor": cursor, "id": "other"}, {"cursor": cursor, "offset": 2001}, {"cursor": cursor, "result_ref": "mutated"}, {"cursor": "expired"}, {"cursor": nil}} {
		if _, err := tool.Execute(t.Context(), args); err == nil {
			t.Fatal("ambiguous or invalid cursor accepted")
		}
	}
	tool.Wire(func(_ context.Context, q taskbridge.ResultQuery) (taskbridge.ResultPage, error) {
		if q.Ref != long {
			t.Fatal("lost old reference")
		}
		return taskbridge.ResultPage{}, fmt.Errorf("not shared with this parent")
	})
	_, err = tool.Execute(t.Context(), result.NextCall)
	if err == nil || !strings.Contains(err.Error(), "not shared with this parent") || !strings.Contains(err.Error(), "recovery_call") || !strings.Contains(err.Error(), `"attempt_id":"attempt"`) {
		t.Fatalf("scope bypass or missing pinned recovery: %v", err)
	}
}

func TestManualContinuationCannotSwitchToLatestAttempt(t *testing.T) {
	tool := NewResultTool()
	attempt := "old"
	reads := 0
	reader := func(_ context.Context, q taskbridge.ResultQuery) (taskbridge.ResultPage, error) {
		reads++
		return taskbridge.ResultPage{ID: "task", AttemptID: attempt, Ref: "ref-" + attempt, Content: "evidence", Offset: q.Offset, NextOffset: 100, HasMore: true}, nil
	}
	tool.Wire(reader)
	if _, err := tool.Execute(t.Context(), map[string]any{"id": "task"}); err != nil {
		t.Fatal(err)
	}
	for _, rebind := range []bool{false, true} {
		if rebind {
			attempt = "new"
			tool.Wire(reader)
		}
		before := reads
		if _, err := tool.Execute(t.Context(), map[string]any{"id": "task", "offset": float64(100)}); err == nil {
			t.Fatal("continuation without an exact attempt was accepted")
		}
		if reads != before {
			t.Fatal("ambiguous continuation reached the reader")
		}
	}
}
