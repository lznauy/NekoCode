package task

import (
	"context"
	"encoding/json"
	"fmt"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"strings"
	"testing"
)

func TestTaskResultPagesStayBelowToolBudgetWithoutTruncation(t *testing.T) {
	tool := NewResultTool()
	tool.Wire(func(_ context.Context, q taskbridge.ResultQuery) (taskbridge.ResultPage, error) {
		return taskbridge.ResultPage{ID: q.ID, AttemptID: "attempt", Ref: "pinned-ref", Status: "completed", Content: strings.Repeat("\x00", 2000), NextOffset: 2000, Total: 4000, HasMore: true}, nil
	})
	output, err := tool.Execute(t.Context(), map[string]any{"id": "task", "limit": float64(2000)})
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Text) > core.MaxOutputBytes {
		t.Fatal("page will be truncated by executor")
	}
	var page taskbridge.ResultPage
	if json.Unmarshal([]byte(output.Text), &page) != nil || len(page.Content) != 2000 || page.NextOffset != 2000 || !page.HasMore {
		t.Fatal("structured pagination was truncated")
	}
	for _, args := range []map[string]any{nil, {"id": "task", "offset": float64(1)}, {"id": "task", "limit": float64(2001)}, {"id": "task", "limit": true}, {"id": "task", "attempt_id": 12}} {
		if _, err := tool.Execute(t.Context(), args); err == nil {
			t.Fatalf("invalid pagination accepted: %v", args)
		}
	}
}

func TestTaskResultContinuationUsesIssuedReferenceWithoutCopyingOpaqueToken(t *testing.T) {
	tool := NewResultTool()
	tool.Wire(func(_ context.Context, q taskbridge.ResultQuery) (taskbridge.ResultPage, error) {
		if q.Offset > 0 && q.Ref != "long-exact-pinned-reference" {
			t.Errorf("continuation lost pinned identity: %+v", q)
		}
		return taskbridge.ResultPage{ID: "task", AttemptID: "attempt", Ref: "long-exact-pinned-reference", Status: "completed", Content: "page", Offset: q.Offset, NextOffset: q.Offset + 100, Total: 200, HasMore: q.Offset == 0}, nil
	})
	first, err := tool.Execute(t.Context(), map[string]any{"id": "task"})
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		NextCall map[string]any `json:"next_call"`
	}
	if err := json.Unmarshal([]byte(first.Text), &output); err != nil {
		t.Fatal(err)
	}
	if output.NextCall == nil {
		t.Error("missing ready-to-use continuation call")
	}
	second, err := tool.Execute(t.Context(), output.NextCall)
	if err != nil {
		t.Fatalf("cannot continue with issued cursor: %v", err)
	}
	var page taskbridge.ResultPage
	if err := json.Unmarshal([]byte(second.Text), &page); err != nil || page.Offset != 100 || page.HasMore {
		t.Fatalf("wrong continuation: %s %v", second.Text, err)
	}
}

func TestTaskResultExplicitContinuationRequiresReference(t *testing.T) {
	tool := NewResultTool()
	reads := 0
	tool.Wire(func(_ context.Context, q taskbridge.ResultQuery) (taskbridge.ResultPage, error) {
		reads++
		return taskbridge.ResultPage{ID: q.ID, AttemptID: q.AttemptID, Ref: q.Ref, Offset: q.Offset, Content: "page"}, nil
	})
	args := map[string]any{"id": "task", "attempt_id": "attempt", "offset": float64(100)}
	if _, err := tool.Execute(t.Context(), args); err == nil || reads != 0 {
		t.Fatal("implicit continuation reached the reader")
	}
	args["result_ref"] = "pinned-ref"
	if _, err := tool.Execute(t.Context(), args); err != nil || reads != 1 {
		t.Fatalf("explicit reference could not recover without a cache: %v", err)
	}
	if len(tool.pages) != 0 {
		t.Fatal("terminal page consumed continuation capacity")
	}
}

func TestTaskResultCursorCapacityKeepsRecentlyUsedPage(t *testing.T) {
	tool := NewResultTool()
	tool.Wire(func(_ context.Context, q taskbridge.ResultQuery) (taskbridge.ResultPage, error) {
		if q.Offset > 0 && q.Ref != "ref-"+q.ID {
			t.Fatalf("cursor lost exact reference: %+v", q)
		}
		return taskbridge.ResultPage{ID: q.ID, AttemptID: "attempt", Ref: "ref-" + q.ID, Content: "page", Offset: q.Offset, NextOffset: 1, HasMore: true}, nil
	})
	read := func(args map[string]any) map[string]any {
		t.Helper()
		out, err := tool.Execute(t.Context(), args)
		if err != nil {
			t.Fatal(err)
		}
		var page struct {
			NextCall map[string]any `json:"next_call"`
		}
		if err := json.Unmarshal([]byte(out.Text), &page); err != nil {
			t.Fatal(err)
		}
		return page.NextCall
	}
	var oldest, evicted map[string]any
	for i := 0; i < resultCacheLimit; i++ {
		next := read(map[string]any{"id": fmt.Sprint(i)})
		if i == 0 {
			oldest = next
		}
		if i == 1 {
			evicted = next
		}
	}
	read(oldest)
	read(map[string]any{"id": "new"})
	if _, err := tool.Execute(t.Context(), evicted); err == nil {
		t.Fatal("least recently used cursor survived eviction")
	}
	read(oldest)
	if len(tool.pages) != resultCacheLimit {
		t.Fatal("cursor cache is unbounded")
	}
}

func TestTaskResultNextCallSurvivesReaderRebind(t *testing.T) {
	tool := NewResultTool()
	reader := func(_ context.Context, q taskbridge.ResultQuery) (taskbridge.ResultPage, error) {
		if q.Offset > 0 && (q.Ref != "pinned-old-attempt" || q.AttemptID != "old-attempt") {
			t.Fatalf("lost pinned result: %+v", q)
		}
		return taskbridge.ResultPage{ID: "task", AttemptID: "old-attempt", Ref: "pinned-old-attempt", Content: "page", Offset: q.Offset, NextOffset: 100, HasMore: q.Offset == 0}, nil
	}
	tool.Wire(reader)
	first, err := tool.Execute(t.Context(), map[string]any{"id": "task"})
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		NextCall map[string]any `json:"next_call"`
	}
	if err := json.Unmarshal([]byte(first.Text), &page); err != nil {
		t.Fatal(err)
	}
	tool.Wire(reader) // Explicit cursors remain usable across coordinator runs.
	if _, err := tool.Execute(t.Context(), page.NextCall); err != nil {
		t.Fatalf("self-contained continuation failed: %v", err)
	}
}

func TestTaskResultCursorAcceptsConsistentArguments(t *testing.T) {
	tool := NewResultTool()
	var got taskbridge.ResultQuery
	tool.Wire(func(_ context.Context, q taskbridge.ResultQuery) (taskbridge.ResultPage, error) {
		got = q
		return taskbridge.ResultPage{ID: "task", AttemptID: "attempt", Ref: "ref", Offset: q.Offset, NextOffset: 100, HasMore: q.Offset == 0}, nil
	})
	first, err := tool.Execute(t.Context(), map[string]any{"id": "task"})
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		NextCall map[string]any `json:"next_call"`
	}
	if err := json.Unmarshal([]byte(first.Text), &page); err != nil {
		t.Fatal(err)
	}
	args := page.NextCall
	args["id"] = "task"
	args["limit"] = float64(2000)
	if _, err := tool.Execute(t.Context(), args); err != nil {
		t.Fatalf("redundant task ID and page size rejected: %v", err)
	}
	if got.ID != "task" || got.AttemptID != "attempt" || got.Ref != "ref" || got.Offset != 100 || got.Limit != 2000 {
		t.Fatalf("wrong pinned query: %+v", got)
	}
	for _, tc := range []struct {
		key   string
		value any
	}{
		{"id", "other"}, {"attempt_id", "other"}, {"result_ref", "other"}, {"offset", float64(0)}, {"limit", float64(2001)}, {"limit", true},
	} {
		bad := map[string]any{"cursor": args["cursor"], tc.key: tc.value}
		got = taskbridge.ResultQuery{}
		if _, err := tool.Execute(t.Context(), bad); err == nil {
			t.Fatalf("invalid cursor arguments accepted: %v", bad)
		}
		if got != (taskbridge.ResultQuery{}) {
			t.Fatalf("invalid arguments reached reader: %+v", got)
		}
	}
}
