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
	second, err := tool.Execute(t.Context(), map[string]any{"id": "task", "attempt_id": "attempt", "offset": float64(100)})
	if err != nil {
		t.Fatalf("cannot continue with issued task/attempt identity: %v", err)
	}
	var page taskbridge.ResultPage
	if err := json.Unmarshal([]byte(second.Text), &page); err != nil || page.Offset != 100 || page.HasMore {
		t.Fatalf("wrong continuation: %s %v", second.Text, err)
	}
}

func TestTaskResultContinuationDoesNotCrossAttemptOrReaderScope(t *testing.T) {
	tool := NewResultTool()
	reader := func(ref string) taskbridge.ResultReader {
		return func(_ context.Context, q taskbridge.ResultQuery) (taskbridge.ResultPage, error) {
			if q.Offset > 0 && q.Ref != ref {
				t.Errorf("wrong scope reference: %+v", q)
			}
			return taskbridge.ResultPage{ID: q.ID, AttemptID: "attempt", Ref: ref, Content: "page", HasMore: true, NextOffset: 100}, nil
		}
	}
	tool.Wire(reader("old-parent-ref"))
	if _, err := tool.Execute(t.Context(), map[string]any{"id": "task"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range []map[string]any{
		{"id": "other", "attempt_id": "attempt", "offset": float64(100)},
		{"id": "task", "attempt_id": "different-attempt", "offset": float64(100)},
	} {
		if _, err := tool.Execute(t.Context(), args); err == nil {
			t.Fatal("reused a different task/attempt reference")
		}
	}
	tool.Wire(reader("new-parent-ref"))
	next := map[string]any{"id": "task", "attempt_id": "attempt", "offset": float64(100)}
	if _, err := tool.Execute(t.Context(), next); err == nil {
		t.Fatal("reused previous reader scope's reference")
	}
	if _, err := tool.Execute(t.Context(), map[string]any{"id": "task"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(t.Context(), next); err != nil {
		t.Fatal(err)
	}
}

func TestTaskResultReferenceCacheCapacityAndOverwrite(t *testing.T) {
	tool := NewResultTool()
	ref := "original"
	tool.Wire(func(_ context.Context, q taskbridge.ResultQuery) (taskbridge.ResultPage, error) {
		return taskbridge.ResultPage{ID: q.ID, AttemptID: "attempt", Ref: ref, Content: "result"}, nil
	})
	for i := 0; i < 128; i++ {
		id := fmt.Sprintf("task-%d", i)
		if _, err := tool.Execute(t.Context(), map[string]any{"id": id}); err != nil {
			t.Fatal(err)
		}
	}
	ref = "updated"
	if _, err := tool.Execute(t.Context(), map[string]any{"id": "task-0"}); err != nil {
		t.Fatal(err)
	}
	if len(tool.refs) != 128 || tool.refs[resultPageKey{"task-0", "attempt"}] != "updated" {
		t.Fatal("overwrite evicted an unrelated reference")
	}
	if _, err := tool.Execute(t.Context(), map[string]any{"id": "task-new"}); err != nil {
		t.Fatal(err)
	}
	if len(tool.refs) != 128 || tool.refs[resultPageKey{"task-new", "attempt"}] != "updated" {
		t.Fatal("cache growth is unbounded or new reference missing")
	}
}

func TestResultReferenceLRUKeepsRecentlyPagedAttempt(t *testing.T) {
	tool := NewResultTool()
	tool.Wire(func(_ context.Context, q taskbridge.ResultQuery) (taskbridge.ResultPage, error) {
		if q.Offset > 0 && q.Ref != "ref-"+q.ID {
			t.Fatalf("paging lost exact reference: %+v", q)
		}
		return taskbridge.ResultPage{ID: q.ID, AttemptID: "attempt", Ref: "ref-" + q.ID, Content: "page", Offset: q.Offset, NextOffset: q.Offset + 1, HasMore: true}, nil
	})
	read := func(id string, offset int) {
		t.Helper()
		if _, err := tool.Execute(t.Context(), map[string]any{"id": id, "attempt_id": "attempt", "offset": float64(offset)}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 128; i++ {
		read(fmt.Sprint(i), 0)
	}
	read("0", 1) // Oldest inserted, but now most recently used.
	read("new", 0)
	if tool.refs[resultPageKey{"0", "attempt"}] == "" || tool.refs[resultPageKey{"1", "attempt"}] != "" {
		t.Fatal("cache did not evict least recently used reference")
	}
	read("0", 2)
	tool.Wire(func(context.Context, taskbridge.ResultQuery) (taskbridge.ResultPage, error) {
		return taskbridge.ResultPage{}, nil
	})
	if len(tool.refs) != 0 || len(tool.recent) != 0 {
		t.Fatal("reader scope change retained cached references")
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
	tool.Wire(reader) // A new coordinator run has no in-memory reference cache.
	if _, err := tool.Execute(t.Context(), page.NextCall); err != nil {
		t.Fatalf("self-contained continuation failed: %v", err)
	}
}
